package client

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMeshTenantDeletion(t *testing.T) {
	markedForDeletion := MeshTenantLifecycle{
		State:             TenantLifecycleStateMarkedForDeletion,
		MarkedForDeletion: &MeshTenantLifecycleAction{Timestamp: "2026-07-30T16:14:14Z"},
	}
	tests := []struct {
		name      string
		tenant    *MeshTenant
		wantDone  bool
		wantState string
	}{
		{"a purged tenant, which answers 404, is deleted", nil, true, tenantNotObserved},
		{"a deleted tenant that meshStack still returns is deleted", &MeshTenant{Status: MeshTenantStatus{Lifecycle: MeshTenantLifecycle{State: TenantLifecycleStateDeleted}}}, true, "DELETED"},
		{"a tenant marked for deletion is not deleted yet, and says since when", &MeshTenant{Status: MeshTenantStatus{Lifecycle: markedForDeletion}}, false, "MARKED_FOR_DELETION since 2026-07-30T16:14:14Z"},
		{"a tenant marked for deletion at no time is not deleted yet, and says what it awaits", &MeshTenant{Status: MeshTenantStatus{Lifecycle: MeshTenantLifecycle{State: TenantLifecycleStateMarkedForDeletion}}}, false, "MARKED_FOR_DELETION, awaiting"},
		{"an active tenant is not deleted, as meshStack has not acted on it", &MeshTenant{Status: MeshTenantStatus{Lifecycle: MeshTenantLifecycle{State: TenantLifecycleStateActive}}}, false, "has not acted on it"},
		{"a tenant without a lifecycle is not deleted, as meshStack has not acted on it", &MeshTenant{Metadata: MeshTenantMetadata{Uuid: "test-uuid"}}, false, "has not acted on it"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			done, err := tt.tenant.DeletionSuccessful()
			require.NoError(t, err)
			assert.Equal(t, tt.wantDone, done)
			assert.Contains(t, tt.tenant.DeletionState(), tt.wantState)
		})
	}
}
