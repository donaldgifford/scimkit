package scim

// Core and extension schema URNs (RFC 7643 §3.3, §4).
const (
	SchemaUser                  = "urn:ietf:params:scim:schemas:core:2.0:User"
	SchemaGroup                 = "urn:ietf:params:scim:schemas:core:2.0:Group"
	SchemaEnterpriseUser        = "urn:ietf:params:scim:schemas:extension:enterprise:2.0:User"
	SchemaServiceProviderConfig = "urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig"
	SchemaResourceType          = "urn:ietf:params:scim:schemas:core:2.0:ResourceType"
	SchemaSchema                = "urn:ietf:params:scim:schemas:core:2.0:Schema"
)

// Protocol message URNs (RFC 7644 §3, §8.2).
const (
	MessageListResponse  = "urn:ietf:params:scim:api:messages:2.0:ListResponse"
	MessageSearchRequest = "urn:ietf:params:scim:api:messages:2.0:SearchRequest"
	MessagePatchOp       = "urn:ietf:params:scim:api:messages:2.0:PatchOp"
	MessageBulkRequest   = "urn:ietf:params:scim:api:messages:2.0:BulkRequest"
	MessageBulkResponse  = "urn:ietf:params:scim:api:messages:2.0:BulkResponse"
	MessageError         = "urn:ietf:params:scim:api:messages:2.0:Error"
)
