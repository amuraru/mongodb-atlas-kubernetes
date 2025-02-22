package customresource

import (
	"fmt"
	"testing"

	"github.com/mongodb/mongodb-atlas-kubernetes/pkg/api/v1/status"
	"github.com/mongodb/mongodb-atlas-kubernetes/pkg/version"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "github.com/mongodb/mongodb-atlas-kubernetes/pkg/api/v1"
)

func TestResourceShouldBeLeftInAtlas(t *testing.T) {
	t.Run("Empty annotations", func(t *testing.T) {
		assert.False(t, ResourceShouldBeLeftInAtlas(&v1.AtlasDatabaseUser{}))
	})

	t.Run("Other annotations", func(t *testing.T) {
		assert.False(t, ResourceShouldBeLeftInAtlas(&v1.AtlasDatabaseUser{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{"foo": "bar"},
			},
		}))
	})

	t.Run("Annotation present, resources should be removed", func(t *testing.T) {
		assert.False(t, ResourceShouldBeLeftInAtlas(&v1.AtlasDatabaseUser{
			ObjectMeta: metav1.ObjectMeta{
				// Any other value except for "keep" is considered as "purge"
				Annotations: map[string]string{ResourcePolicyAnnotation: "foobar"},
			},
		}))
	})

	t.Run("Annotation present, resources should be kept", func(t *testing.T) {
		assert.True(t, ResourceShouldBeLeftInAtlas(&v1.AtlasDatabaseUser{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{ResourcePolicyAnnotation: ResourcePolicyKeep},
			},
		}))
	})
}

func TestReconciliationShouldBeSkipped(t *testing.T) {
	newResourceTypes := func() []v1.AtlasCustomResource {
		return []v1.AtlasCustomResource{
			&v1.AtlasDeployment{},
			&v1.AtlasDatabaseUser{},
			&v1.AtlasProject{},
		}
	}

	t.Run("Empty annotations", func(t *testing.T) {
		for _, resourceType := range newResourceTypes() {
			assert.False(t, ReconciliationShouldBeSkipped(resourceType))
		}
	})

	t.Run("Other resource types", func(t *testing.T) {
		for _, resourceType := range newResourceTypes() {
			resourceType.SetAnnotations(map[string]string{"foo": "bar"})
			assert.False(t, ReconciliationShouldBeSkipped(resourceType))
		}
	})

	t.Run("Annotation present, reconciliation should not be skipped", func(t *testing.T) {
		for _, resourceType := range newResourceTypes() {
			resourceType.SetAnnotations(map[string]string{ReconciliationPolicyAnnotation: "foobar"})
			assert.False(t, ReconciliationShouldBeSkipped(resourceType))
		}
	})

	t.Run("Annotation present, reconciliation should be skipped", func(t *testing.T) {
		for _, resourceType := range newResourceTypes() {
			resourceType.SetAnnotations(map[string]string{ReconciliationPolicyAnnotation: ReconciliationPolicySkip})
			assert.True(t, ReconciliationShouldBeSkipped(resourceType))
		}
	})
}

func TestResourceVersionIsValid(t *testing.T) {
	tests := []struct {
		name            string
		resource        v1.AtlasCustomResource
		want            bool
		wantErr         assert.ErrorAssertionFunc
		operatorVersion string
	}{
		{
			name: "Resource version is LOWER than operator version",
			resource: &v1.AtlasProject{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AtlasProject",
					APIVersion: "atlas.mongodb.com/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "TestProject",
					Labels: map[string]string{
						ResourceVersion: "1.3.0",
					},
				},
				Spec:   v1.AtlasProjectSpec{},
				Status: status.AtlasProjectStatus{},
			},
			want:            true,
			operatorVersion: "1.4.0",
			wantErr:         assert.NoError,
		},
		{
			name: "Resource version is EQUAL to the operator version",
			resource: &v1.AtlasProject{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AtlasProject",
					APIVersion: "atlas.mongodb.com/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "TestProject",
					Labels: map[string]string{
						ResourceVersion: "1.3.0",
					},
				},
				Spec:   v1.AtlasProjectSpec{},
				Status: status.AtlasProjectStatus{},
			},
			want:            true,
			operatorVersion: "1.3.0",
			wantErr:         assert.NoError,
		},
		{
			name: "Resource version is GREATER than the operator version",
			resource: &v1.AtlasProject{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AtlasProject",
					APIVersion: "atlas.mongodb.com/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "TestProject",
					Labels: map[string]string{
						ResourceVersion: "1.5.0",
					},
				},
				Spec:   v1.AtlasProjectSpec{},
				Status: status.AtlasProjectStatus{},
			},
			want:            false,
			operatorVersion: "1.3.0",
			wantErr:         assert.NoError,
		},
		{
			name: "Resource version is GREATER than the operator version with ALLOWED OVERRIDE",
			resource: &v1.AtlasProject{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AtlasProject",
					APIVersion: "atlas.mongodb.com/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "TestProject",
					Labels: map[string]string{
						ResourceVersion: "1.5.0",
					},
					Annotations: map[string]string{
						ResourceVersionOverride: ResourceVersionAllow,
					},
				},
				Spec:   v1.AtlasProjectSpec{},
				Status: status.AtlasProjectStatus{},
			},
			want:            true,
			operatorVersion: "1.3.0",
			wantErr:         assert.NoError,
		},
		{
			name: "Resource version is GREATER than the operator version with DISALLOWED OVERRIDE",
			resource: &v1.AtlasProject{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AtlasProject",
					APIVersion: "atlas.mongodb.com/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "TestProject",
					Labels: map[string]string{
						ResourceVersion: "1.5.0",
					},
					Annotations: map[string]string{
						ResourceVersionOverride: "someValue",
					},
				},
				Spec:   v1.AtlasProjectSpec{},
				Status: status.AtlasProjectStatus{},
			},
			want:            false,
			operatorVersion: "1.3.0",
			wantErr:         assert.NoError,
		},
		{
			name: "Resource version is INCORRECT, should return an error",
			resource: &v1.AtlasProject{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AtlasProject",
					APIVersion: "atlas.mongodb.com/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "TestProject",
					Labels: map[string]string{
						ResourceVersion: "1.incorrect.semantic.version",
					},
				},
				Spec:   v1.AtlasProjectSpec{},
				Status: status.AtlasProjectStatus{},
			},
			want:            false,
			operatorVersion: "1.3.0",
			wantErr:         assert.Error,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version.Version = tt.operatorVersion
			got, err := ResourceVersionIsValid(tt.resource)
			if !tt.wantErr(t, err, fmt.Sprintf("ResourceVersionIsValid(%v)", tt.resource)) {
				return
			}
			assert.Equalf(t, tt.want, got, "ResourceVersionIsValid(%v)", tt.resource)
		})
	}
}
