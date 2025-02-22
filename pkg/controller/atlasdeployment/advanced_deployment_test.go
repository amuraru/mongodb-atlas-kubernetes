package atlasdeployment

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"go.uber.org/zap"

	"github.com/mongodb/mongodb-atlas-kubernetes/pkg/api/v1/common"
	"github.com/mongodb/mongodb-atlas-kubernetes/pkg/api/v1/status"
	"github.com/mongodb/mongodb-atlas-kubernetes/pkg/controller/workflow"
	"github.com/mongodb/mongodb-atlas-kubernetes/pkg/util/toptr"

	"github.com/stretchr/testify/assert"
	"go.mongodb.org/atlas/mongodbatlas"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	mdbv1 "github.com/mongodb/mongodb-atlas-kubernetes/pkg/api/v1"
)

func TestMergedAdvancedDeployment(t *testing.T) {
	defaultAtlas := makeDefaultAtlasSpec()
	atlasRegionConfig := defaultAtlas.ReplicationSpecs[0].RegionConfigs[0]
	fillInSpecs(atlasRegionConfig, "M10", "AWS")

	t.Run("Test merging clusters removes backing provider name if empty", func(t *testing.T) {
		advancedCluster := mdbv1.DefaultAwsAdvancedDeployment("default", "my-project")

		merged, _, err := MergedAdvancedDeployment(*defaultAtlas, *advancedCluster.Spec.AdvancedDeploymentSpec)
		assert.NoError(t, err)
		assert.Empty(t, merged.ReplicationSpecs[0].RegionConfigs[0].BackingProviderName)
	})

	t.Run("Test merging clusters does not remove backing provider name if it is present in the atlas type", func(t *testing.T) {
		atlasRegionConfig.ElectableSpecs.InstanceSize = "M5"
		atlasRegionConfig.ProviderName = "TENANT"
		atlasRegionConfig.BackingProviderName = "AWS"

		advancedCluster := mdbv1.DefaultAwsAdvancedDeployment("default", "my-project")
		advancedCluster.Spec.AdvancedDeploymentSpec.ReplicationSpecs[0].RegionConfigs[0].ElectableSpecs.InstanceSize = "M5"
		advancedCluster.Spec.AdvancedDeploymentSpec.ReplicationSpecs[0].RegionConfigs[0].ProviderName = "TENANT"
		advancedCluster.Spec.AdvancedDeploymentSpec.ReplicationSpecs[0].RegionConfigs[0].BackingProviderName = "AWS"

		merged, _, err := MergedAdvancedDeployment(*defaultAtlas, *advancedCluster.Spec.AdvancedDeploymentSpec)
		assert.NoError(t, err)
		assert.Equal(t, atlasRegionConfig.BackingProviderName, merged.ReplicationSpecs[0].RegionConfigs[0].BackingProviderName)
	})
}

func TestAdvancedDeploymentsEqual(t *testing.T) {
	defaultAtlas := makeDefaultAtlasSpec()
	regionConfig := defaultAtlas.ReplicationSpecs[0].RegionConfigs[0]
	fillInSpecs(regionConfig, "M10", "AWS")

	t.Run("Test ", func(t *testing.T) {
		advancedCluster := mdbv1.DefaultAwsAdvancedDeployment("default", "my-project")

		merged, atlas, err := MergedAdvancedDeployment(*defaultAtlas, *advancedCluster.Spec.AdvancedDeploymentSpec)
		assert.NoError(t, err)

		logger, _ := zap.NewProduction()
		areEqual, _ := AdvancedDeploymentsEqual(logger.Sugar(), merged, atlas)
		assert.True(t, areEqual, "Deploymnts should be equal")
	})
}

func makeDefaultAtlasSpec() *mongodbatlas.AdvancedCluster {
	return &mongodbatlas.AdvancedCluster{
		ClusterType: "REPLICASET",
		Name:        "test-deployment-advanced",
		ReplicationSpecs: []*mongodbatlas.AdvancedReplicationSpec{
			{
				NumShards: 1,
				ID:        "123",
				ZoneName:  "Zone1",
				RegionConfigs: []*mongodbatlas.AdvancedRegionConfig{
					{
						ElectableSpecs: &mongodbatlas.Specs{
							InstanceSize: "M10",
							NodeCount:    toptr.MakePtr(3),
						},
						Priority:     toptr.MakePtr(7),
						ProviderName: "AWS",
						RegionName:   "US_EAST_1",
					},
				},
			},
		},
	}
}

func fillInSpecs(regionConfig *mongodbatlas.AdvancedRegionConfig, instanceSize string, provider string) {
	regionConfig.ProviderName = provider

	regionConfig.ElectableSpecs.InstanceSize = instanceSize
	regionConfig.AnalyticsSpecs = &mongodbatlas.Specs{
		InstanceSize: instanceSize,
		NodeCount:    toptr.MakePtr(0),
	}
	regionConfig.ReadOnlySpecs = &mongodbatlas.Specs{
		InstanceSize: instanceSize,
		NodeCount:    toptr.MakePtr(0),
	}
}

func TestAdvancedDeployment_handleAutoscaling(t *testing.T) {
	testCases := []struct {
		desiredDeployment *mdbv1.AdvancedDeploymentSpec
		currentDeployment *mongodbatlas.AdvancedCluster
		expected          *mdbv1.AdvancedDeploymentSpec
		shouldFail        bool
		testName          string
		err               error
	}{
		{
			testName: "One region and autoscaling ENABLED for compute AND disk",
			desiredDeployment: &mdbv1.AdvancedDeploymentSpec{
				DiskSizeGB: toptr.MakePtr(15),
				ReplicationSpecs: []*mdbv1.AdvancedReplicationSpec{
					{
						NumShards: 1,
						ZoneName:  "us-east-1",
						RegionConfigs: []*mdbv1.AdvancedRegionConfig{
							{
								ElectableSpecs: &mdbv1.Specs{
									DiskIOPS:      nil,
									EbsVolumeType: "",
									InstanceSize:  "M30",
									NodeCount:     toptr.MakePtr(1),
								},
								AutoScaling: &mdbv1.AdvancedAutoScalingSpec{
									DiskGB: &mdbv1.DiskGB{
										Enabled: toptr.MakePtr(true),
									},
									Compute: &mdbv1.ComputeSpec{
										Enabled:          toptr.MakePtr(true),
										ScaleDownEnabled: nil,
										MinInstanceSize:  "M10",
										MaxInstanceSize:  "M40",
									},
								},
							},
						},
					},
				},
			},
			currentDeployment: &mongodbatlas.AdvancedCluster{
				ReplicationSpecs: []*mongodbatlas.AdvancedReplicationSpec{
					{
						RegionConfigs: []*mongodbatlas.AdvancedRegionConfig{
							{
								ElectableSpecs: &mongodbatlas.Specs{
									InstanceSize: "M30",
								},
							},
						},
					},
				},
			},
			expected: &mdbv1.AdvancedDeploymentSpec{
				DiskSizeGB: nil,
				ReplicationSpecs: []*mdbv1.AdvancedReplicationSpec{
					{
						NumShards: 1,
						ZoneName:  "us-east-1",
						RegionConfigs: []*mdbv1.AdvancedRegionConfig{
							{
								ElectableSpecs: &mdbv1.Specs{
									DiskIOPS:      nil,
									EbsVolumeType: "",
									InstanceSize:  "",
									NodeCount:     toptr.MakePtr(1),
								},
								AutoScaling: &mdbv1.AdvancedAutoScalingSpec{
									DiskGB: &mdbv1.DiskGB{
										Enabled: toptr.MakePtr(true),
									},
									Compute: &mdbv1.ComputeSpec{
										Enabled:          toptr.MakePtr(true),
										ScaleDownEnabled: nil,
										MinInstanceSize:  "M10",
										MaxInstanceSize:  "M40",
									},
								},
							},
						},
					},
				},
			},
			shouldFail: false,
		},
		{
			testName: "One region and autoscaling ENABLED for compute ONLY",
			desiredDeployment: &mdbv1.AdvancedDeploymentSpec{
				DiskSizeGB: toptr.MakePtr(15),
				ReplicationSpecs: []*mdbv1.AdvancedReplicationSpec{
					{
						NumShards: 1,
						ZoneName:  "us-east-1",
						RegionConfigs: []*mdbv1.AdvancedRegionConfig{
							{
								ElectableSpecs: &mdbv1.Specs{
									DiskIOPS:      nil,
									EbsVolumeType: "",
									InstanceSize:  "M40",
									NodeCount:     toptr.MakePtr(1),
								},
								AutoScaling: &mdbv1.AdvancedAutoScalingSpec{
									DiskGB: &mdbv1.DiskGB{
										Enabled: toptr.MakePtr(false),
									},
									Compute: &mdbv1.ComputeSpec{
										Enabled:          toptr.MakePtr(true),
										ScaleDownEnabled: nil,
										MinInstanceSize:  "M10",
										MaxInstanceSize:  "M40",
									},
								},
							},
						},
					},
				},
			},
			currentDeployment: &mongodbatlas.AdvancedCluster{
				ReplicationSpecs: []*mongodbatlas.AdvancedReplicationSpec{
					{
						RegionConfigs: []*mongodbatlas.AdvancedRegionConfig{
							{
								ElectableSpecs: &mongodbatlas.Specs{
									InstanceSize: "M40",
								},
							},
						},
					},
				},
			},
			expected: &mdbv1.AdvancedDeploymentSpec{
				DiskSizeGB: toptr.MakePtr(15),
				ReplicationSpecs: []*mdbv1.AdvancedReplicationSpec{
					{
						NumShards: 1,
						ZoneName:  "us-east-1",
						RegionConfigs: []*mdbv1.AdvancedRegionConfig{
							{
								ElectableSpecs: &mdbv1.Specs{
									DiskIOPS:      nil,
									EbsVolumeType: "",
									InstanceSize:  "",
									NodeCount:     toptr.MakePtr(1),
								},
								AutoScaling: &mdbv1.AdvancedAutoScalingSpec{
									DiskGB: &mdbv1.DiskGB{
										Enabled: toptr.MakePtr(false),
									},
									Compute: &mdbv1.ComputeSpec{
										Enabled:          toptr.MakePtr(true),
										ScaleDownEnabled: nil,
										MinInstanceSize:  "M10",
										MaxInstanceSize:  "M40",
									},
								},
							},
						},
					},
				},
			},
			shouldFail: false,
		},
		{
			testName: "One region and autoscaling ENABLED for diskGB ONLY",
			desiredDeployment: &mdbv1.AdvancedDeploymentSpec{
				DiskSizeGB: toptr.MakePtr(15),
				ReplicationSpecs: []*mdbv1.AdvancedReplicationSpec{
					{
						NumShards: 1,
						ZoneName:  "us-east-1",
						RegionConfigs: []*mdbv1.AdvancedRegionConfig{
							{
								ElectableSpecs: &mdbv1.Specs{
									DiskIOPS:      nil,
									EbsVolumeType: "",
									InstanceSize:  "M40",
									NodeCount:     toptr.MakePtr(1),
								},
								AutoScaling: &mdbv1.AdvancedAutoScalingSpec{
									DiskGB: &mdbv1.DiskGB{
										Enabled: toptr.MakePtr(true),
									},
									Compute: &mdbv1.ComputeSpec{
										Enabled:          toptr.MakePtr(false),
										ScaleDownEnabled: nil,
										MinInstanceSize:  "M10",
										MaxInstanceSize:  "M40",
									},
								},
							},
						},
					},
				},
			},
			expected: &mdbv1.AdvancedDeploymentSpec{
				DiskSizeGB: nil,
				ReplicationSpecs: []*mdbv1.AdvancedReplicationSpec{
					{
						NumShards: 1,
						ZoneName:  "us-east-1",
						RegionConfigs: []*mdbv1.AdvancedRegionConfig{
							{
								ElectableSpecs: &mdbv1.Specs{
									DiskIOPS:      nil,
									EbsVolumeType: "",
									InstanceSize:  "M40",
									NodeCount:     toptr.MakePtr(1),
								},
								AutoScaling: &mdbv1.AdvancedAutoScalingSpec{
									DiskGB: &mdbv1.DiskGB{
										Enabled: toptr.MakePtr(true),
									},
									Compute: &mdbv1.ComputeSpec{
										Enabled:          toptr.MakePtr(false),
										ScaleDownEnabled: nil,
										MinInstanceSize:  "M10",
										MaxInstanceSize:  "M40",
									},
								},
							},
						},
					},
				},
			},
			shouldFail: false,
		},
		{
			testName: "Two regions and autoscaling ENABLED for compute AND disk in different regions",
			desiredDeployment: &mdbv1.AdvancedDeploymentSpec{
				DiskSizeGB: toptr.MakePtr(15),
				ReplicationSpecs: []*mdbv1.AdvancedReplicationSpec{
					{
						NumShards: 1,
						ZoneName:  "us-east-1",
						RegionConfigs: []*mdbv1.AdvancedRegionConfig{
							{
								RegionName: "WESTERN_EUROPE",
								ElectableSpecs: &mdbv1.Specs{
									DiskIOPS:      nil,
									EbsVolumeType: "",
									InstanceSize:  "M30",
									NodeCount:     toptr.MakePtr(1),
								},
								AutoScaling: &mdbv1.AdvancedAutoScalingSpec{
									DiskGB: &mdbv1.DiskGB{
										Enabled: toptr.MakePtr(true),
									},
									Compute: &mdbv1.ComputeSpec{
										Enabled:          toptr.MakePtr(true),
										ScaleDownEnabled: nil,
										MinInstanceSize:  "M10",
										MaxInstanceSize:  "M40",
									},
								},
							},
							{
								RegionName: "EASTERN_EUROPE",
								ElectableSpecs: &mdbv1.Specs{
									DiskIOPS:      nil,
									EbsVolumeType: "",
									InstanceSize:  "M30",
									NodeCount:     toptr.MakePtr(1),
								},
								AutoScaling: &mdbv1.AdvancedAutoScalingSpec{
									DiskGB: &mdbv1.DiskGB{
										Enabled: toptr.MakePtr(true),
									},
									Compute: &mdbv1.ComputeSpec{
										Enabled:          toptr.MakePtr(true),
										ScaleDownEnabled: nil,
										MinInstanceSize:  "M10",
										MaxInstanceSize:  "M40",
									},
								},
							},
						},
					},
				},
			},
			currentDeployment: &mongodbatlas.AdvancedCluster{
				ReplicationSpecs: []*mongodbatlas.AdvancedReplicationSpec{
					{
						RegionConfigs: []*mongodbatlas.AdvancedRegionConfig{
							{
								ElectableSpecs: &mongodbatlas.Specs{
									InstanceSize: "M30",
								},
							},
							{
								ElectableSpecs: &mongodbatlas.Specs{
									InstanceSize: "M30",
								},
							},
						},
					},
				},
			},
			expected: &mdbv1.AdvancedDeploymentSpec{
				DiskSizeGB: nil,
				ReplicationSpecs: []*mdbv1.AdvancedReplicationSpec{
					{
						NumShards: 1,
						ZoneName:  "us-east-1",
						RegionConfigs: []*mdbv1.AdvancedRegionConfig{
							{
								RegionName: "WESTERN_EUROPE",
								ElectableSpecs: &mdbv1.Specs{
									DiskIOPS:      nil,
									EbsVolumeType: "",
									InstanceSize:  "",
									NodeCount:     toptr.MakePtr(1),
								},
								AutoScaling: &mdbv1.AdvancedAutoScalingSpec{
									DiskGB: &mdbv1.DiskGB{
										Enabled: toptr.MakePtr(true),
									},
									Compute: &mdbv1.ComputeSpec{
										Enabled:          toptr.MakePtr(true),
										ScaleDownEnabled: nil,
										MinInstanceSize:  "M10",
										MaxInstanceSize:  "M40",
									},
								},
							},
							{
								RegionName: "EASTERN_EUROPE",
								ElectableSpecs: &mdbv1.Specs{
									DiskIOPS:      nil,
									EbsVolumeType: "",
									InstanceSize:  "",
									NodeCount:     toptr.MakePtr(1),
								},
								AutoScaling: &mdbv1.AdvancedAutoScalingSpec{
									DiskGB: &mdbv1.DiskGB{
										Enabled: toptr.MakePtr(true),
									},
									Compute: &mdbv1.ComputeSpec{
										Enabled:          toptr.MakePtr(true),
										ScaleDownEnabled: nil,
										MinInstanceSize:  "M10",
										MaxInstanceSize:  "M40",
									},
								},
							},
						},
					},
				},
			},
			shouldFail: false,
		},
		{
			testName: "One region and autoscaling DISABLED for diskGB AND compute",
			desiredDeployment: &mdbv1.AdvancedDeploymentSpec{
				DiskSizeGB: toptr.MakePtr(15),
				ReplicationSpecs: []*mdbv1.AdvancedReplicationSpec{
					{
						NumShards: 1,
						ZoneName:  "us-east-1",
						RegionConfigs: []*mdbv1.AdvancedRegionConfig{
							{
								ElectableSpecs: &mdbv1.Specs{
									DiskIOPS:      nil,
									EbsVolumeType: "",
									InstanceSize:  "M20",
									NodeCount:     toptr.MakePtr(1),
								},
								AutoScaling: &mdbv1.AdvancedAutoScalingSpec{
									DiskGB: &mdbv1.DiskGB{
										Enabled: toptr.MakePtr(false),
									},
									Compute: &mdbv1.ComputeSpec{
										Enabled:          toptr.MakePtr(false),
										ScaleDownEnabled: nil,
										MinInstanceSize:  "M10",
										MaxInstanceSize:  "M40",
									},
								},
							},
						},
					},
				},
			},
			currentDeployment: &mongodbatlas.AdvancedCluster{
				ReplicationSpecs: []*mongodbatlas.AdvancedReplicationSpec{
					{
						RegionConfigs: []*mongodbatlas.AdvancedRegionConfig{
							{
								ElectableSpecs: &mongodbatlas.Specs{
									InstanceSize: "M20",
								},
							},
						},
					},
				},
			},
			expected: &mdbv1.AdvancedDeploymentSpec{
				DiskSizeGB: toptr.MakePtr(15),
				ReplicationSpecs: []*mdbv1.AdvancedReplicationSpec{
					{
						NumShards: 1,
						ZoneName:  "us-east-1",
						RegionConfigs: []*mdbv1.AdvancedRegionConfig{
							{
								ElectableSpecs: &mdbv1.Specs{
									DiskIOPS:      nil,
									EbsVolumeType: "",
									InstanceSize:  "M20",
									NodeCount:     toptr.MakePtr(1),
								},
								AutoScaling: &mdbv1.AdvancedAutoScalingSpec{
									DiskGB: &mdbv1.DiskGB{
										Enabled: toptr.MakePtr(false),
									},
									Compute: &mdbv1.ComputeSpec{
										Enabled:          toptr.MakePtr(false),
										ScaleDownEnabled: nil,
										MinInstanceSize:  "M10",
										MaxInstanceSize:  "M40",
									},
								},
							},
						},
					},
				},
			},
			shouldFail: false,
		},
		{
			testName: "One regions and autoscaling ENABLED for compute and InstanceSize outside of min boundary",
			desiredDeployment: &mdbv1.AdvancedDeploymentSpec{
				DiskSizeGB: toptr.MakePtr(15),
				ReplicationSpecs: []*mdbv1.AdvancedReplicationSpec{
					{
						NumShards: 1,
						ZoneName:  "us-east-1",
						RegionConfigs: []*mdbv1.AdvancedRegionConfig{
							{
								RegionName: "WESTERN_EUROPE",
								ElectableSpecs: &mdbv1.Specs{
									DiskIOPS:      nil,
									EbsVolumeType: "",
									InstanceSize:  "M10",
									NodeCount:     toptr.MakePtr(1),
								},
								AutoScaling: &mdbv1.AdvancedAutoScalingSpec{
									DiskGB: &mdbv1.DiskGB{
										Enabled: toptr.MakePtr(true),
									},
									Compute: &mdbv1.ComputeSpec{
										Enabled:          toptr.MakePtr(true),
										ScaleDownEnabled: nil,
										MinInstanceSize:  "M20",
										MaxInstanceSize:  "M40",
									},
								},
							},
						},
					},
				},
			},
			currentDeployment: &mongodbatlas.AdvancedCluster{
				ReplicationSpecs: []*mongodbatlas.AdvancedReplicationSpec{
					{
						RegionConfigs: []*mongodbatlas.AdvancedRegionConfig{
							{
								ElectableSpecs: &mongodbatlas.Specs{
									InstanceSize: "M10",
								},
							},
						},
					},
				},
			},
			expected: &mdbv1.AdvancedDeploymentSpec{
				DiskSizeGB: nil,
				ReplicationSpecs: []*mdbv1.AdvancedReplicationSpec{
					{
						NumShards: 1,
						ZoneName:  "us-east-1",
						RegionConfigs: []*mdbv1.AdvancedRegionConfig{
							{
								RegionName: "WESTERN_EUROPE",
								ElectableSpecs: &mdbv1.Specs{
									DiskIOPS:      nil,
									EbsVolumeType: "",
									InstanceSize:  "M20",
									NodeCount:     toptr.MakePtr(1),
								},
								AutoScaling: &mdbv1.AdvancedAutoScalingSpec{
									DiskGB: &mdbv1.DiskGB{
										Enabled: toptr.MakePtr(true),
									},
									Compute: &mdbv1.ComputeSpec{
										Enabled:          toptr.MakePtr(true),
										ScaleDownEnabled: nil,
										MinInstanceSize:  "M20",
										MaxInstanceSize:  "M40",
									},
								},
							},
						},
					},
				},
			},
			shouldFail: false,
		},
		{
			testName: "One regions and autoscaling ENABLED for compute and InstanceSize outside of max boundary",
			desiredDeployment: &mdbv1.AdvancedDeploymentSpec{
				DiskSizeGB: toptr.MakePtr(15),
				ReplicationSpecs: []*mdbv1.AdvancedReplicationSpec{
					{
						NumShards: 1,
						ZoneName:  "us-east-1",
						RegionConfigs: []*mdbv1.AdvancedRegionConfig{
							{
								RegionName: "WESTERN_EUROPE",
								ElectableSpecs: &mdbv1.Specs{
									DiskIOPS:      nil,
									EbsVolumeType: "",
									InstanceSize:  "M50",
									NodeCount:     toptr.MakePtr(1),
								},
								AutoScaling: &mdbv1.AdvancedAutoScalingSpec{
									DiskGB: &mdbv1.DiskGB{
										Enabled: toptr.MakePtr(true),
									},
									Compute: &mdbv1.ComputeSpec{
										Enabled:          toptr.MakePtr(true),
										ScaleDownEnabled: nil,
										MinInstanceSize:  "M10",
										MaxInstanceSize:  "M40",
									},
								},
							},
						},
					},
				},
			},
			currentDeployment: &mongodbatlas.AdvancedCluster{
				ReplicationSpecs: []*mongodbatlas.AdvancedReplicationSpec{
					{
						RegionConfigs: []*mongodbatlas.AdvancedRegionConfig{
							{
								ElectableSpecs: &mongodbatlas.Specs{
									InstanceSize: "M50",
								},
							},
						},
					},
				},
			},
			expected: &mdbv1.AdvancedDeploymentSpec{
				DiskSizeGB: nil,
				ReplicationSpecs: []*mdbv1.AdvancedReplicationSpec{
					{
						NumShards: 1,
						ZoneName:  "us-east-1",
						RegionConfigs: []*mdbv1.AdvancedRegionConfig{
							{
								RegionName: "WESTERN_EUROPE",
								ElectableSpecs: &mdbv1.Specs{
									DiskIOPS:      nil,
									EbsVolumeType: "",
									InstanceSize:  "M40",
									NodeCount:     toptr.MakePtr(1),
								},
								AutoScaling: &mdbv1.AdvancedAutoScalingSpec{
									DiskGB: &mdbv1.DiskGB{
										Enabled: toptr.MakePtr(true),
									},
									Compute: &mdbv1.ComputeSpec{
										Enabled:          toptr.MakePtr(true),
										ScaleDownEnabled: nil,
										MinInstanceSize:  "M10",
										MaxInstanceSize:  "M40",
									},
								},
							},
						},
					},
				},
			},
			shouldFail: false,
		},
		{
			testName: "One region and autoscaling with wrong configuration",
			desiredDeployment: &mdbv1.AdvancedDeploymentSpec{
				DiskSizeGB: toptr.MakePtr(15),
				ReplicationSpecs: []*mdbv1.AdvancedReplicationSpec{
					{
						NumShards: 1,
						ZoneName:  "us-east-1",
						RegionConfigs: []*mdbv1.AdvancedRegionConfig{
							{
								ElectableSpecs: &mdbv1.Specs{
									DiskIOPS:      nil,
									EbsVolumeType: "",
									InstanceSize:  "M30",
									NodeCount:     toptr.MakePtr(1),
								},
								AutoScaling: &mdbv1.AdvancedAutoScalingSpec{
									DiskGB: &mdbv1.DiskGB{
										Enabled: toptr.MakePtr(true),
									},
									Compute: &mdbv1.ComputeSpec{
										Enabled:          toptr.MakePtr(true),
										ScaleDownEnabled: nil,
										MinInstanceSize:  "S10",
										MaxInstanceSize:  "M40",
									},
								},
							},
						},
					},
				},
			},
			currentDeployment: &mongodbatlas.AdvancedCluster{
				ReplicationSpecs: []*mongodbatlas.AdvancedReplicationSpec{
					{
						RegionConfigs: []*mongodbatlas.AdvancedRegionConfig{
							{
								ElectableSpecs: &mongodbatlas.Specs{
									InstanceSize: "M30",
								},
							},
						},
					},
				},
			},
			expected: &mdbv1.AdvancedDeploymentSpec{
				DiskSizeGB: nil,
				ReplicationSpecs: []*mdbv1.AdvancedReplicationSpec{
					{
						NumShards: 1,
						ZoneName:  "us-east-1",
						RegionConfigs: []*mdbv1.AdvancedRegionConfig{
							{
								ElectableSpecs: &mdbv1.Specs{
									DiskIOPS:      nil,
									EbsVolumeType: "",
									InstanceSize:  "M30",
									NodeCount:     toptr.MakePtr(1),
								},
								AutoScaling: &mdbv1.AdvancedAutoScalingSpec{
									DiskGB: &mdbv1.DiskGB{
										Enabled: toptr.MakePtr(true),
									},
									Compute: &mdbv1.ComputeSpec{
										Enabled:          toptr.MakePtr(true),
										ScaleDownEnabled: nil,
										MinInstanceSize:  "S10",
										MaxInstanceSize:  "M40",
									},
								},
							},
						},
					},
				},
			},
			shouldFail: true,
			err:        errors.New("instance size is invalid. instance family should be M or R"),
		},
	}
	for _, tt := range testCases {
		t.Run(tt.testName, func(t *testing.T) {
			ctx := workflow.NewContext(zap.S(), []status.Condition{})
			err := handleAutoscaling(ctx, tt.desiredDeployment, tt.currentDeployment)

			assert.Equal(t, tt.err, err)
			if !reflect.DeepEqual(tt.desiredDeployment, tt.expected) && !tt.shouldFail {
				expJSON, err := json.MarshalIndent(tt.expected, "", " ")
				if err != nil {
					t.Fatalf("err: %v", err)
				}
				inpJSON, err := json.MarshalIndent(tt.desiredDeployment, "", " ")
				if err != nil {
					t.Fatalf("err: %v", err)
				}
				t.Errorf("FAIL. Expected: %v, Got: %v", string(expJSON), string(inpJSON))
			}
		})
	}
}

func TestNormalizeInstanceSize(t *testing.T) {
	t.Run("InstanceSizeName should not change when inside of autoscaling configuration boundaries", func(t *testing.T) {
		ctx := workflow.NewContext(zap.S(), []status.Condition{})
		autoscaling := &mdbv1.AdvancedAutoScalingSpec{
			Compute: &mdbv1.ComputeSpec{
				Enabled:          toptr.MakePtr(true),
				ScaleDownEnabled: toptr.MakePtr(true),
				MinInstanceSize:  "M10",
				MaxInstanceSize:  "M30",
			},
		}

		size, err := normalizeInstanceSize(ctx, "M10", autoscaling)

		assert.NoError(t, err)
		assert.Equal(t, "M10", size)
	})
	t.Run("InstanceSizeName should change to minimum size when outside of the bottom autoscaling configuration boundaries", func(t *testing.T) {
		ctx := workflow.NewContext(zap.S(), []status.Condition{})
		autoscaling := &mdbv1.AdvancedAutoScalingSpec{
			Compute: &mdbv1.ComputeSpec{
				Enabled:          toptr.MakePtr(true),
				ScaleDownEnabled: toptr.MakePtr(true),
				MinInstanceSize:  "M20",
				MaxInstanceSize:  "M30",
			},
		}

		size, err := normalizeInstanceSize(ctx, "M10", autoscaling)

		assert.NoError(t, err)
		assert.Equal(t, "M20", size)
	})
	t.Run("InstanceSizeName should change to maximum size when outside of the top autoscaling configuration boundaries", func(t *testing.T) {
		ctx := workflow.NewContext(zap.S(), []status.Condition{})
		autoscaling := &mdbv1.AdvancedAutoScalingSpec{
			Compute: &mdbv1.ComputeSpec{
				Enabled:          toptr.MakePtr(true),
				ScaleDownEnabled: toptr.MakePtr(true),
				MinInstanceSize:  "M20",
				MaxInstanceSize:  "M30",
			},
		}

		size, err := normalizeInstanceSize(ctx, "M40", autoscaling)

		assert.NoError(t, err)
		assert.Equal(t, "M30", size)
	})
}

func TestDbUserBelongsToProjects(t *testing.T) {
	t.Run("Database User refer to a different project name", func(*testing.T) {
		dbUser := &mdbv1.AtlasDatabaseUser{
			Spec: mdbv1.AtlasDatabaseUserSpec{
				Project: common.ResourceRefNamespaced{
					Name: "project2",
				},
			},
		}
		project := &mdbv1.AtlasProject{
			ObjectMeta: v1.ObjectMeta{
				Name: "project1",
			},
		}

		assert.False(t, dbUserBelongsToProject(dbUser, project))
	})

	t.Run("Database User is no", func(*testing.T) {
		dbUser := &mdbv1.AtlasDatabaseUser{
			ObjectMeta: v1.ObjectMeta{
				Namespace: "ns-2",
			},
			Spec: mdbv1.AtlasDatabaseUserSpec{
				Project: common.ResourceRefNamespaced{
					Name: "project1",
				},
			},
		}
		project := &mdbv1.AtlasProject{
			ObjectMeta: v1.ObjectMeta{
				Name:      "project1",
				Namespace: "ns-1",
			},
		}

		assert.False(t, dbUserBelongsToProject(dbUser, project))
	})

	t.Run("Database User refer to a project with same name but in another namespace", func(*testing.T) {
		dbUser := &mdbv1.AtlasDatabaseUser{
			Spec: mdbv1.AtlasDatabaseUserSpec{
				Project: common.ResourceRefNamespaced{
					Name:      "project1",
					Namespace: "ns-2",
				},
			},
		}
		project := &mdbv1.AtlasProject{
			ObjectMeta: v1.ObjectMeta{
				Name:      "project1",
				Namespace: "ns-1",
			},
		}

		assert.False(t, dbUserBelongsToProject(dbUser, project))
	})

	t.Run("Database User refer to a valid project and namespace", func(*testing.T) {
		dbUser := &mdbv1.AtlasDatabaseUser{
			Spec: mdbv1.AtlasDatabaseUserSpec{
				Project: common.ResourceRefNamespaced{
					Name:      "project1",
					Namespace: "ns-1",
				},
			},
		}
		project := &mdbv1.AtlasProject{
			ObjectMeta: v1.ObjectMeta{
				Name:      "project1",
				Namespace: "ns-1",
			},
		}

		assert.True(t, dbUserBelongsToProject(dbUser, project))
	})
}
