package resource

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authlib "github.com/grafana/authlib/types"

	"github.com/grafana/grafana/pkg/apimachinery/identity"
	"github.com/grafana/grafana/pkg/apimachinery/utils"
)

func TestAuthzLimitedClient_BatchCheck(t *testing.T) {
	t.Run("RBAC compatible resources should use underlying client", func(t *testing.T) {
		mockClient := authlib.FixedAccessClient(false)
		client := NewAuthzLimitedClient(mockClient, AuthzOptions{})

		req := authlib.BatchCheckRequest{
			Namespace: "stacks-1",
			Checks: []authlib.BatchCheckItem{
				{CorrelationID: "check1", Group: "dashboard.grafana.app", Resource: "dashboards", Verb: utils.VerbGet, Name: "dash1"},
				{CorrelationID: "check2", Group: "folder.grafana.app", Resource: "folders", Verb: utils.VerbGet, Name: "folder1"},
			},
		}

		resp, err := client.BatchCheck(context.Background(), &identity.StaticRequester{Namespace: "stacks-1"}, req)
		require.NoError(t, err)
		assert.Len(t, resp.Results, 2)
		assert.False(t, resp.Results["check1"].Allowed)
		assert.False(t, resp.Results["check2"].Allowed)
	})

	t.Run("non-RBAC compatible resources should be allowed", func(t *testing.T) {
		mockClient := authlib.FixedAccessClient(false)
		client := NewAuthzLimitedClient(mockClient, AuthzOptions{})

		req := authlib.BatchCheckRequest{
			Namespace: "stacks-1",
			Checks: []authlib.BatchCheckItem{
				{CorrelationID: "check1", Group: "unknown.group", Resource: "unknown.resource", Verb: utils.VerbGet, Name: "item1"},
				{CorrelationID: "check2", Group: "another.group", Resource: "another.resource", Verb: utils.VerbGet, Name: "item2"},
			},
		}

		resp, err := client.BatchCheck(context.Background(), &identity.StaticRequester{Namespace: "stacks-1"}, req)
		require.NoError(t, err)
		assert.Len(t, resp.Results, 2)
		assert.True(t, resp.Results["check1"].Allowed)
		assert.True(t, resp.Results["check2"].Allowed)
	})

	t.Run("mixed resources - some RBAC compatible, some not", func(t *testing.T) {
		mockClient := authlib.FixedAccessClient(false)
		client := NewAuthzLimitedClient(mockClient, AuthzOptions{})

		req := authlib.BatchCheckRequest{
			Namespace: "stacks-1",
			Checks: []authlib.BatchCheckItem{
				{CorrelationID: "check1", Group: "dashboard.grafana.app", Resource: "dashboards", Verb: utils.VerbGet, Name: "dash1"},
				{CorrelationID: "check2", Group: "unknown.group", Resource: "unknown.resource", Verb: utils.VerbGet, Name: "item1"},
				{CorrelationID: "check3", Group: "folder.grafana.app", Resource: "folders", Verb: utils.VerbGet, Name: "folder1"},
			},
		}

		resp, err := client.BatchCheck(context.Background(), &identity.StaticRequester{Namespace: "stacks-1"}, req)
		require.NoError(t, err)
		assert.Len(t, resp.Results, 3)
		// RBAC compatible - should be denied (mockClient returns false)
		assert.False(t, resp.Results["check1"].Allowed)
		// Not RBAC compatible - should be allowed
		assert.True(t, resp.Results["check2"].Allowed)
		// RBAC compatible - should be denied (mockClient returns false)
		assert.False(t, resp.Results["check3"].Allowed)
	})

	t.Run("RBAC compatible resources with allowed client", func(t *testing.T) {
		mockClient := authlib.FixedAccessClient(true)
		client := NewAuthzLimitedClient(mockClient, AuthzOptions{})

		req := authlib.BatchCheckRequest{
			Namespace: "stacks-1",
			Checks: []authlib.BatchCheckItem{
				{CorrelationID: "check1", Group: "dashboard.grafana.app", Resource: "dashboards", Verb: utils.VerbGet, Name: "dash1"},
			},
		}

		resp, err := client.BatchCheck(context.Background(), &identity.StaticRequester{Namespace: "stacks-1"}, req)
		require.NoError(t, err)
		assert.Len(t, resp.Results, 1)
		assert.True(t, resp.Results["check1"].Allowed)
	})
}

func TestValidateAuthzOptions(t *testing.T) {
	t.Run("empty options are valid", func(t *testing.T) {
		require.NoError(t, ValidateAuthzOptions(AuthzOptions{}))
	})

	t.Run("valid exemptions", func(t *testing.T) {
		require.NoError(t, ValidateAuthzOptions(AuthzOptions{
			ExemptionEnabled: true,
			ExemptResources:  []string{"playlist.grafana.app/playlists", " shorturl.grafana.app/shorturls "},
		}))
	})

	t.Run("malformed exemptions", func(t *testing.T) {
		cases := []string{
			"",
			"   ",
			"playlist.grafana.app",
			"playlist.grafana.app/playlists/extra",
			"/playlists",
			"playlist.grafana.app/",
			"*",
			"*.grafana.app/playlists",
			"playlist.grafana.app/*",
			"*/*",
		}
		for _, entry := range cases {
			t.Run(fmt.Sprintf("%q", entry), func(t *testing.T) {
				err := ValidateAuthzOptions(AuthzOptions{
					ExemptionEnabled: true,
					ExemptResources:  []string{entry},
				})
				require.Error(t, err, "entry %q", entry)
			})
		}
	})

	t.Run("always-enforced resources cannot be exempted", func(t *testing.T) {
		cases := []string{
			"dashboard.grafana.app/dashboards",
			"folder.grafana.app/folders",
			"iam.grafana.app/users",
			"iam.grafana.app/teams",
			"iam.grafana.app/serviceaccounts",
			"widget.ext.grafana.app/widgets",
			"customcrdtest.ext.grafana.app/things",
		}
		for _, entry := range cases {
			t.Run(entry, func(t *testing.T) {
				err := ValidateAuthzOptions(AuthzOptions{
					ExemptionEnabled: true,
					ExemptResources:  []string{entry},
				})
				require.Error(t, err, "entry %q", entry)
			})
		}
	})

	t.Run("one invalid entry fails the whole list", func(t *testing.T) {
		err := ValidateAuthzOptions(AuthzOptions{
			ExemptionEnabled: true,
			ExemptResources:  []string{"playlist.grafana.app/playlists", "not-valid"},
		})
		require.Error(t, err)
	})

	t.Run("allowlist sibling is exemptable", func(t *testing.T) {
		require.NoError(t, ValidateAuthzOptions(AuthzOptions{
			ExemptionEnabled: true,
			ExemptResources:  []string{"iam.grafana.app/roles"},
		}))
	})
}

func TestAuthzLimitedClient_ExemptionMode(t *testing.T) {
	mockClient := authlib.FixedAccessClient(false)

	t.Run("gate off ignores exemptions", func(t *testing.T) {
		client := NewAuthzLimitedClient(mockClient, AuthzOptions{
			ExemptionEnabled: false,
			ExemptResources:  []string{"playlist.grafana.app/playlists"},
		})

		tests := []struct {
			group, resource string
			allowed         bool
		}{
			{"dashboard.grafana.app", "dashboards", false},
			{"folder.grafana.app", "folders", false},
			{"iam.grafana.app", "users", false},
			{"widget.ext.grafana.app", "widgets", false},
			{"playlist.grafana.app", "playlists", true},
			{"unknown.group", "unknown.resource", true},
		}
		for _, tt := range tests {
			req := authlib.CheckRequest{
				Group: tt.group, Resource: tt.resource, Verb: utils.VerbGet, Namespace: "stacks-1",
			}
			resp, err := client.Check(context.Background(), &identity.StaticRequester{Namespace: "stacks-1"}, req, "")
			require.NoError(t, err)
			assert.Equal(t, tt.allowed, resp.Allowed, "%s/%s", tt.group, tt.resource)
		}
	})

	t.Run("gate on enforces unknown unless exact exemption", func(t *testing.T) {
		client := NewAuthzLimitedClient(mockClient, AuthzOptions{
			ExemptionEnabled: true,
			ExemptResources:  []string{"playlist.grafana.app/playlists"},
		})

		tests := []struct {
			group, resource string
			allowed         bool
		}{
			{"dashboard.grafana.app", "dashboards", false},
			{"folder.grafana.app", "folders", false},
			{"iam.grafana.app", "users", false},
			{"iam.grafana.app", "teams", false},
			{"iam.grafana.app", "serviceaccounts", false},
			{"widget.ext.grafana.app", "widgets", false},
			{"playlist.grafana.app", "playlists", true},
			{"playlist.grafana.app", "playlistitems", false}, // sibling stays enforced
			{"unknown.group", "unknown.resource", false},
			{"shorturl.grafana.app", "shorturls", false},
		}
		for _, tt := range tests {
			req := authlib.CheckRequest{
				Group: tt.group, Resource: tt.resource, Verb: utils.VerbGet, Namespace: "stacks-1",
			}
			resp, err := client.Check(context.Background(), &identity.StaticRequester{Namespace: "stacks-1"}, req, "")
			require.NoError(t, err)
			assert.Equal(t, tt.allowed, resp.Allowed, "%s/%s", tt.group, tt.resource)
		}
	})

	t.Run("gate on with empty exemptions enforces everything", func(t *testing.T) {
		client := NewAuthzLimitedClient(mockClient, AuthzOptions{ExemptionEnabled: true})
		req := authlib.CheckRequest{
			Group: "unknown.group", Resource: "unknown.resource", Verb: utils.VerbGet, Namespace: "stacks-1",
		}
		resp, err := client.Check(context.Background(), &identity.StaticRequester{Namespace: "stacks-1"}, req, "")
		require.NoError(t, err)
		assert.False(t, resp.Allowed)
	})

	t.Run("batch check honors exemption mode", func(t *testing.T) {
		client := NewAuthzLimitedClient(mockClient, AuthzOptions{
			ExemptionEnabled: true,
			ExemptResources:  []string{"playlist.grafana.app/playlists"},
		})
		req := authlib.BatchCheckRequest{
			Namespace: "stacks-1",
			Checks: []authlib.BatchCheckItem{
				{CorrelationID: "dash", Group: "dashboard.grafana.app", Resource: "dashboards", Verb: utils.VerbGet, Name: "d1"},
				{CorrelationID: "pl", Group: "playlist.grafana.app", Resource: "playlists", Verb: utils.VerbGet, Name: "p1"},
				{CorrelationID: "sibling", Group: "playlist.grafana.app", Resource: "playlistitems", Verb: utils.VerbGet, Name: "i1"},
				{CorrelationID: "unknown", Group: "unknown.group", Resource: "unknown.resource", Verb: utils.VerbGet, Name: "u1"},
				{CorrelationID: "ext", Group: "widget.ext.grafana.app", Resource: "widgets", Verb: utils.VerbGet, Name: "w1"},
			},
		}
		resp, err := client.BatchCheck(context.Background(), &identity.StaticRequester{Namespace: "stacks-1"}, req)
		require.NoError(t, err)
		assert.False(t, resp.Results["dash"].Allowed)
		assert.True(t, resp.Results["pl"].Allowed)
		assert.False(t, resp.Results["sibling"].Allowed)
		assert.False(t, resp.Results["unknown"].Allowed)
		assert.False(t, resp.Results["ext"].Allowed)
	})

	t.Run("compile honors exemption mode", func(t *testing.T) {
		client := NewAuthzLimitedClient(mockClient, AuthzOptions{
			ExemptionEnabled: true,
			ExemptResources:  []string{"playlist.grafana.app/playlists"},
		})

		//nolint:staticcheck // SA1019: Compile is deprecated but BatchCheck is not yet fully implemented
		exemptChecker, _, err := client.Compile(context.Background(), &identity.StaticRequester{Namespace: "stacks-1"}, authlib.ListRequest{
			Group: "playlist.grafana.app", Resource: "playlists", Verb: utils.VerbGet, Namespace: "stacks-1",
		})
		require.NoError(t, err)
		assert.True(t, exemptChecker("name", "folder"))

		//nolint:staticcheck // SA1019: Compile is deprecated but BatchCheck is not yet fully implemented
		enforcedChecker, _, err := client.Compile(context.Background(), &identity.StaticRequester{Namespace: "stacks-1"}, authlib.ListRequest{
			Group: "unknown.group", Resource: "unknown.resource", Verb: utils.VerbGet, Namespace: "stacks-1",
		})
		require.NoError(t, err)
		assert.False(t, enforcedChecker("name", "folder"))
	})
}

func TestAuthzLimitedClient_Check(t *testing.T) {
	mockClient := authlib.FixedAccessClient(false)
	client := NewAuthzLimitedClient(mockClient, AuthzOptions{})

	tests := []struct {
		group    string
		resource string
		expected bool
	}{
		{"dashboard.grafana.app", "dashboards", false},
		{"folder.grafana.app", "folders", false},
		{"unknown.group", "unknown.resource", true},
	}

	for _, test := range tests {
		req := authlib.CheckRequest{
			Group:     test.group,
			Resource:  test.resource,
			Verb:      utils.VerbGet,
			Namespace: "stacks-1",
		}
		resp, err := client.Check(context.Background(), &identity.StaticRequester{Namespace: "stacks-1"}, req, "")
		assert.NoError(t, err)
		assert.Equal(t, test.expected, resp.Allowed)
	}
}

func TestAuthzLimitedClient_Compile(t *testing.T) {
	mockClient := authlib.FixedAccessClient(false)
	client := NewAuthzLimitedClient(mockClient, AuthzOptions{})

	tests := []struct {
		group    string
		resource string
		expected bool
	}{
		{"dashboard.grafana.app", "dashboards", false},
		{"folder.grafana.app", "folders", false},
		{"unknown.group", "unknown.resource", true},
	}

	for _, test := range tests {
		req := authlib.ListRequest{
			Group:     test.group,
			Resource:  test.resource,
			Verb:      utils.VerbGet,
			Namespace: "stacks-1",
		}
		//nolint:staticcheck // SA1019: Compile is deprecated but BatchCheck is not yet fully implemented
		checker, _, err := client.Compile(context.Background(), &identity.StaticRequester{Namespace: "stacks-1"}, req)
		assert.NoError(t, err)
		assert.NotNil(t, checker)

		result := checker("name", "folder")
		assert.Equal(t, test.expected, result)
	}
}

// TestNamespaceMatching tests namespace matching in Check and Compile methods
func TestNamespaceMatching(t *testing.T) {
	// Create a mock client that always returns allowed=true
	mockClient := authlib.FixedAccessClient(true)
	client := NewAuthzLimitedClient(mockClient, AuthzOptions{})

	// Create a context with fallback disabled
	ctx := context.Background()

	tests := []struct {
		name          string
		authNamespace string
		reqNamespace  string
		expectError   bool
	}{
		{
			name:          "matching namespaces",
			authNamespace: "ns1",
			reqNamespace:  "ns1",
			expectError:   false,
		},
		{
			name:          "mismatched namespaces",
			authNamespace: "ns1",
			reqNamespace:  "ns2",
			expectError:   true,
		},
		{
			name:          "empty request namespace",
			authNamespace: "ns1",
			reqNamespace:  "",
			expectError:   true,
		},
		{
			name:          "empty auth namespace",
			authNamespace: "",
			reqNamespace:  "ns1",
			expectError:   true,
		},
		{
			name:          "wildcard auth namespace",
			authNamespace: "*",
			reqNamespace:  "ns1",
			expectError:   false,
		},
		{
			name:          "both empty namespaces",
			authNamespace: "",
			reqNamespace:  "",
			expectError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test Check method with namespace matching
			checkReq := authlib.CheckRequest{
				Group:     "unknown.group", // Use unknown group to bypass RBAC check
				Resource:  "unknown.resource",
				Verb:      utils.VerbGet,
				Namespace: tt.reqNamespace,
			}
			// Create a mock auth info with the specified namespace
			// Test Check method
			user := &identity.StaticRequester{Namespace: tt.authNamespace}
			_, checkErr := client.Check(ctx, user, checkReq, "")

			// Test Compile method
			compileReq := authlib.ListRequest{
				Group:     "unknown.group", // Use unknown group to bypass RBAC check
				Resource:  "unknown.resource",
				Verb:      utils.VerbGet,
				Namespace: tt.reqNamespace,
			}
			//nolint:staticcheck // SA1019: Compile is deprecated but BatchCheck is not yet fully implemented
			_, _, compileErr := client.Compile(ctx, user, compileReq)

			if tt.expectError {
				require.Error(t, checkErr, "Check should return error")
				require.Error(t, compileErr, "Compile should return error")
				assert.ErrorIs(t, checkErr, authlib.ErrNamespaceMismatch, "Check should return namespace mismatch error")
				assert.ErrorIs(t, compileErr, authlib.ErrNamespaceMismatch, "Compile should return namespace mismatch error")
			} else {
				assert.NoError(t, checkErr, "Check should not return error when namespaces match")
				assert.NoError(t, compileErr, "Compile should not return error when namespaces match")
			}
		})
	}
}
