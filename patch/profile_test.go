package patch

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/scim"
)

// TestProfileDefinitions pins each built-in profile's knob set so a
// definition change is always a deliberate, reviewed diff.
func TestProfileDefinitions(t *testing.T) {
	tests := []struct {
		name    string
		profile Profile
		want    Profile
	}{
		{name: "strict is all off", profile: Strict, want: Profile{}},
		{
			name:    "default is shape tolerance only",
			profile: Default,
			want: Profile{
				CaseInsensitiveOps:     true,
				CoerceStringPrimitives: true,
				ExpandPathlessReplace:  true,
				RemoveValueAsSelector:  true,
			},
		},
		{
			name:    "entra adds the noTarget remappings",
			profile: Entra,
			want: Profile{
				CaseInsensitiveOps:     true,
				CoerceStringPrimitives: true,
				ExpandPathlessReplace:  true,
				RemoveValueAsSelector:  true,
				ReplaceNoTargetAsAdd:   true,
				SynthesizeAddValuePath: true,
			},
		},
		{name: "okta matches default", profile: Okta, want: Default},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.profile)
		})
	}
}

// TestProfileDecodeContrast decodes one representative deviant body per
// tolerant profile and asserts Strict rejects the same bytes.
func TestProfileDecodeContrast(t *testing.T) {
	userRT := scim.UserResourceType()
	groupRT := scim.GroupResourceType()
	tests := []struct {
		name           string
		rt             *scim.ResourceType
		profile        Profile
		body           string
		wantStrictType string
	}{
		{
			name:           "entra title-cased op with wrapped string boolean",
			rt:             &userRT,
			profile:        Entra,
			body:           patchBody(`[{"op": "Replace", "path": "active", "value": [{"$ref": null, "value": "False"}]}]`),
			wantStrictType: scim.ScimTypeInvalidSyntax,
		},
		{
			name:           "okta member removal by value",
			rt:             &groupRT,
			profile:        Okta,
			body:           patchBody(`[{"op": "remove", "path": "members", "value": [{"value": "2819c223"}]}]`),
			wantStrictType: scim.ScimTypeInvalidValue,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decode([]byte(tt.body), tt.rt, tt.profile)
			require.NoError(t, err)
			_, err = Decode([]byte(tt.body), tt.rt, Strict)
			requireScimType(t, err, tt.wantStrictType)
		})
	}
}
