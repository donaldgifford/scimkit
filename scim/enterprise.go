package scim

// Manager is the manager sub-attribute of the enterprise User extension
// (RFC 7643 §4.3). Ref maps the $ref sub-attribute.
type Manager struct {
	Value       string
	Ref         string
	DisplayName string
}

// EnterpriseUser is the typed view of the
// urn:ietf:params:scim:schemas:extension:enterprise:2.0:User extension
// (RFC 7643 §4.3), attached to a User via its Enterprise field.
type EnterpriseUser struct {
	EmployeeNumber string
	CostCenter     string
	Organization   string
	Division       string
	Department     string
	Manager        *Manager
}

// containerValue renders the extension as a container map for Resource.Set,
// or nil when the whole extension is unassigned.
func (e *EnterpriseUser) containerValue() any {
	if e == nil {
		return nil
	}
	container := make(ComplexValue)
	putString(container, "employeenumber", e.EmployeeNumber)
	putString(container, "costcenter", e.CostCenter)
	putString(container, "organization", e.Organization)
	putString(container, "division", e.Division)
	putString(container, "department", e.Department)
	if e.Manager != nil {
		cv := make(ComplexValue)
		putString(cv, "value", e.Manager.Value)
		putString(cv, "$ref", e.Manager.Ref)
		putString(cv, "displayname", e.Manager.DisplayName)
		if len(cv) > 0 {
			container["manager"] = cv
		}
	}
	if len(container) == 0 {
		return nil
	}
	return map[string]any(container)
}

// enterpriseFromResource projects the enterprise extension container into
// the typed view, or nil when the extension holds no data.
func enterpriseFromResource(r *Resource) *EnterpriseUser {
	v, ok := r.Get(SchemaEnterpriseUser)
	if !ok {
		return nil
	}
	container, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	e := &EnterpriseUser{
		EmployeeNumber: stringOf(container["employeenumber"]),
		CostCenter:     stringOf(container["costcenter"]),
		Organization:   stringOf(container["organization"]),
		Division:       stringOf(container["division"]),
		Department:     stringOf(container["department"]),
	}
	if cv, ok := container["manager"].(ComplexValue); ok {
		e.Manager = &Manager{
			Value:       stringOf(cv["value"]),
			Ref:         stringOf(cv["$ref"]),
			DisplayName: stringOf(cv["displayname"]),
		}
	}
	return e
}
