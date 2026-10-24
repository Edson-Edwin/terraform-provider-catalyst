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
	attributes := make(map[string]schema.Attribute)
	for operation, fallback := range map[string]string{"create": "5m", "read": "2m", "update": "2m", "delete": "5m"} {
		attributes[operation] = schema.StringAttribute{
			Optional:            true,
			MarkdownDescription: "Maximum duration for " + operation + ", including requests and polling. Defaults to " + fallback + ". Use a positive Go duration such as 30s or 10m.",
			Validators:          []validator.String{projectDurationValidator{}},
		}
	}
	return schema.SingleNestedAttribute{
		Optional:            true,
		MarkdownDescription: "Operation time budgets. A timeout retains known project state for recovery; it does not replay writes or undo a remote operation.",
		Attributes:          attributes,
	}
}

type projectDurationValidator struct{}

func (projectDurationValidator) Description(context.Context) string {
	return "must be a positive Go duration, such as 30s or 10m"
}
func (v projectDurationValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}
func (v projectDurationValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if d, err := time.ParseDuration(req.ConfigValue.ValueString()); err != nil || d <= 0 {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid project timeout", v.Description(ctx))
	}
}

func projectOperationContext(ctx context.Context, timeouts types.Object, operation string, fallback time.Duration, diags *diag.Diagnostics) (context.Context, context.CancelFunc) {
	attributePath := path.Root("timeouts").AtName(operation)
	if timeouts.IsUnknown() {
		diags.AddAttributeError(path.Root("timeouts"), "Unknown project timeouts", "Timeouts must be known before managing the project.")
	} else if !timeouts.IsNull() {
		value, ok := timeouts.Attributes()[operation].(types.String)
		if !ok || value.IsUnknown() {
			diags.AddAttributeError(attributePath, "Unknown project timeout", "The operation timeout must be known before managing the project.")
		} else if !value.IsNull() {
			duration, err := time.ParseDuration(value.ValueString())
			if err != nil || duration <= 0 {
				diags.AddAttributeError(attributePath, "Invalid project timeout", "Use a positive Go duration, such as 30s or 10m.")
			} else {
				fallback = duration
			}
		}
	}
	return context.WithTimeout(ctx, fallback)
}
