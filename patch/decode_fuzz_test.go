package patch

import (
	"testing"

	"github.com/donaldgifford/scimkit/internal/idpcorpus"
	"github.com/donaldgifford/scimkit/scim"
)

// FuzzDecode drives hostile bytes through the PatchOp decoder under
// every built-in profile, seeded with the golden IdP corpus. Rejecting
// input is fine; the invariants are no panics, every error being a
// *scim.Error, and every successful decode yielding structurally valid
// ops (known kind, value presence matching the kind).
func FuzzDecode(f *testing.F) {
	fixtures, err := idpcorpus.All()
	if err != nil {
		f.Fatalf("load corpus seeds: %v", err)
	}
	for i := range fixtures {
		f.Add([]byte(fixtures[i].Request))
	}
	hostile := []string{
		`{}`,
		`[]`,
		`{"schemas": ["urn:ietf:params:scim:api:messages:2.0:PatchOp"], "Operations": []}`,
		`{"schemas": ["urn:ietf:params:scim:api:messages:2.0:PatchOp"], "Operations": [{"op": "add"}]}`,
		`{"schemas": ["urn:ietf:params:scim:api:messages:2.0:PatchOp"], "Operations": [{"op": "remove", "path": "emails["}]}`,
		`{"schemas": ["urn:ietf:params:scim:api:messages:2.0:PatchOp"], "Operations": [{"op": "replace", "value": {"a.b": 1, "urn:x:y": 2}}]}`,
		`{"schemas": ["urn:ietf:params:scim:api:messages:2.0:PatchOp"], "Operations": [{"op": "remove", "path": "members", "value": [{"value": "x"}]}]}`,
		`{"schemas": [], "Operations": [{"op": "add", "value": 1}]}`,
		`{"Operations": null}`,
		`{"schemas": ["urn:ietf:params:scim:api:messages:2.0:PatchOp"], "Operations": [{"op": "Replace", "path": "active", "value": [{"$ref": null, "value": "False"}]}]}`,
		`{"schemas": ["urn:ietf:params:scim:api:messages:2.0:PatchOp"], "Operations": [{"op": "add", "path": "count", "value": 9007199254740993}]}`,
		"\x00",
		`{"schemas": ["URN:IETF:PARAMS:SCIM:API:MESSAGES:2.0:PATCHOP"], "OPERATIONS": [{"OP": "ADD", "PATH": "title", "VALUE": "x"}]}`,
	}
	for _, seed := range hostile {
		f.Add([]byte(seed))
	}

	userRT := scim.UserResourceType()
	groupRT := scim.GroupResourceType()
	profiles := []Profile{Strict, Default, Entra}
	f.Fuzz(func(t *testing.T, body []byte) {
		for _, rt := range []*scim.ResourceType{&userRT, &groupRT} {
			for i := range profiles {
				ops, err := Decode(body, rt, profiles[i])
				if err != nil {
					var scimErr *scim.Error
					if !asScimError(err, &scimErr) {
						t.Fatalf("Decode error is not a *scim.Error: %v", err)
					}
					continue
				}
				checkOpInvariants(t, ops)
			}
		}
	})
}

func asScimError(err error, target **scim.Error) bool {
	e, ok := err.(*scim.Error) //nolint:errorlint // decode errors are constructed, never wrapped
	if ok {
		*target = e
	}
	return ok
}

func checkOpInvariants(t *testing.T, ops []Op) {
	t.Helper()
	for _, op := range ops {
		switch op.Kind {
		case KindAdd, KindReplace:
			if op.Value == nil {
				t.Fatalf("%s op decoded with nil value", op.Kind)
			}
		case KindRemove:
			if op.Value != nil {
				t.Fatalf("remove op decoded with a value: %v", op.Value)
			}
		default:
			t.Fatalf("decoded unknown kind %q", op.Kind)
		}
	}
}
