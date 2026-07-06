// Package server is a net/http toolkit for building SCIM 2.0 service
// providers (RFC 7644). Consumers implement the minimal Store contract per
// resource type; the package provides routing, request decoding and
// normalization, schema validation, PATCH application, filter evaluation
// fallbacks, attribute projection, ETags, and generated discovery
// endpoints (ServiceProviderConfig, ResourceTypes, Schemas).
package server
