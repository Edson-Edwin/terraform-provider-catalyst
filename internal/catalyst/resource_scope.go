// SPDX-License-Identifier: MPL-2.0
package catalyst

import "errors"

// ResourceRoutingScope returns explicit, stable routing for managed resources.
// It does not claim to attest the authenticated caller's identity.
func (c *Client) ResourceRoutingScope() (dataCenter, environment, organizationID string, err error) {
	if c.config.OrganizationID == "" {
		return "", "", "", errors.New("managed resources require an explicit organization_id or CATALYST_ORGANIZATION_ID; account-default routing cannot be bound safely to resource state")
	}
	return c.config.DataCenter, c.config.Environment, c.config.OrganizationID, nil
}
