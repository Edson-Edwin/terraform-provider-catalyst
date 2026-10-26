// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func projectTimeoutsSchema() schema.SingleNestedAttribute {
	return operationTimeoutsSchema("project", map[string]string{"create": "5m", "read": "2m", "update": "2m", "delete": "5m"})
}

func operationTimeoutsSchema(resourceName string, defaults map[string]string) schema.SingleNestedAttribute {
	attributes := make(map[string]schema.Attribute)
	for operation, fallback := range defaults {
		attributes[operation] = schema.StringAttribute{
			Optional:            true,
			MarkdownDescription: "Maximum duration for " + operation + ", including requests and polling. Defaults to " + fallback + ". Use a positive Go duration such as 30s or 10m.",
			Validators:          []validator.String{operationDurationValidator{resourceName: resourceName}},
		}
	}
	return schema.SingleNestedAttribute{
		Optional:            true,
		MarkdownDescription: "Operation time budgets. A timeout retains known " + resourceName + " state for recovery; it does not replay writes or undo a remote operation.",
		Attributes:          attributes,
	}
}

type operationDurationValidator struct{ resourceName string }

func (operationDurationValidator) Description(context.Context) string {
	return "must be a positive Go duration, such as 30s or 10m"
}
func (v operationDurationValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}
func (v operationDurationValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if d, err := time.ParseDuration(req.ConfigValue.ValueString()); err != nil || d <= 0 {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid "+v.resourceName+" timeout", v.Description(ctx))
	}
}

func projectOperationContext(ctx context.Context, timeouts types.Object, operation string, fallback time.Duration, diags *diag.Diagnostics) (context.Context, context.CancelFunc) {
	return operationContext(ctx, timeouts, operation, fallback, "project", diags)
}

func operationContext(ctx context.Context, timeouts types.Object, operation string, fallback time.Duration, resourceName string, diags *diag.Diagnostics) (context.Context, context.CancelFunc) {
	attributePath := path.Root("timeouts").AtName(operation)
	if timeouts.IsUnknown() {
		diags.AddAttributeError(path.Root("timeouts"), "Unknown "+resourceName+" timeouts", "Timeouts must be known before managing the "+resourceName+".")
	} else if !timeouts.IsNull() {
		value, ok := timeouts.Attributes()[operation].(types.String)
		if !ok || value.IsUnknown() {
			diags.AddAttributeError(attributePath, "Unknown "+resourceName+" timeout", "The operation timeout must be known before managing the "+resourceName+".")
		} else if !value.IsNull() {
			duration, err := time.ParseDuration(value.ValueString())
			if err != nil || duration <= 0 {
				diags.AddAttributeError(attributePath, "Invalid "+resourceName+" timeout", "Use a positive Go duration, such as 30s or 10m.")
			} else {
				fallback = duration
			}
		}
	}
	return context.WithTimeout(ctx, fallback)
}
