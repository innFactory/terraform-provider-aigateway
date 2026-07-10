package provider

import (
	"fmt"

	aigateway "github.com/innFactory/aigateway-go"
)

// The provider no longer carries its own HTTP transport: the request path,
// dual-header admin-key auth and error mapping all live in the shared SDK
// (github.com/innFactory/aigateway-go), which was lifted verbatim from this
// provider so there is a single, well-tested client. This file only adapts the
// SDK surface to the names the provider (and its tests) already use.

// apiError aliases the SDK's structured gateway error so existing references
// (and tests constructing &apiError{Status: ...}) keep compiling.
type apiError = aigateway.APIError

// isNotFound / isConflict delegate to the SDK's 404/409 classifiers. Kept as
// package-level vars (not thin wrappers) so call sites read unchanged.
var (
	isNotFound = aigateway.IsNotFound
	isConflict = aigateway.IsConflict
)

// newClient builds an SDK client authenticating with the shared full-admin key
// (dual Authorization: Bearer + X-Gateway-Admin-Key headers) and the provider's
// User-Agent. This is the only construction seam — the transport itself is the
// SDK's.
func newClient(endpoint, adminKey, version string) *aigateway.Client {
	return aigateway.New(
		endpoint,
		aigateway.AdminKeyCredential(adminKey),
		aigateway.WithUserAgent(fmt.Sprintf("terraform-provider-aigateway/%s", version)),
	)
}
