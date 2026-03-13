package openroutersdk

import "testing"

func TestRenamedPublicTypesCompile(t *testing.T) {
	var _ *OpenRouterAgentOptions
	var _ OpenRouterSDKError
	var _ Model
	var _ ModelInfo
	var _ ModelListResponse
	var _ UserInputCallback
	var _ SessionStat
}
