package v1beta1

import (
	"testing"

	. "github.com/onsi/gomega"
	osconfigv1 "github.com/openshift/api/config/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/utils/pointer"
	aws "sigs.k8s.io/cluster-api-provider-aws/pkg/apis/awsprovider/v1beta1"
	azure "sigs.k8s.io/cluster-api-provider-azure/pkg/apis/azureprovider/v1beta1"
	gcp "sigs.k8s.io/cluster-api-provider-gcp/pkg/apis/gcpprovider/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

func TestMachineSetCreation(t *testing.T) {
	g := NewWithT(t)

	// Override config getter
	ctrl.GetConfig = func() (*rest.Config, error) {
		return cfg, nil
	}
	defer func() {
		ctrl.GetConfig = config.GetConfig
	}()

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "machineset-creation-test",
		},
	}
	g.Expect(c.Create(ctx, namespace)).To(Succeed())
	defer func() {
		g.Expect(c.Delete(ctx, namespace)).To(Succeed())
	}()

	infra := &osconfigv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
	}
	g.Expect(c.Create(ctx, infra)).To(Succeed())
	defer func() {
		g.Expect(c.Delete(ctx, infra)).To(Succeed())
	}()

	testCases := []struct {
		name              string
		platformType      osconfigv1.PlatformType
		clusterID         string
		expectedError     string
		providerSpecValue *runtime.RawExtension
	}{
		{
			name:              "with AWS and a nil provider spec value",
			platformType:      osconfigv1.AWSPlatformType,
			clusterID:         "aws-cluster",
			providerSpecValue: nil,
			expectedError:     "providerSpec.value: Required value: a value must be provided",
		},
		{
			name:         "with AWS and no fields set",
			platformType: osconfigv1.AWSPlatformType,
			clusterID:    "aws-cluster",
			providerSpecValue: &runtime.RawExtension{
				Object: &aws.AWSMachineProviderConfig{},
			},
			expectedError: "providerSpec.ami: Required value: expected either providerSpec.ami.arn or providerSpec.ami.filters or providerSpec.ami.id to be populated",
		},
		{
			name:         "with AWS and an AMI ID set",
			platformType: osconfigv1.AWSPlatformType,
			clusterID:    "aws-cluster",
			providerSpecValue: &runtime.RawExtension{
				Object: &aws.AWSMachineProviderConfig{
					AMI: aws.AWSResourceReference{
						ID: pointer.StringPtr("ami"),
					},
				},
			},
			expectedError: "",
		},
		{
			name:              "with Azure and a nil provider spec value",
			platformType:      osconfigv1.AWSPlatformType,
			clusterID:         "azure-cluster",
			providerSpecValue: nil,
			expectedError:     "providerSpec.value: Required value: a value must be provided",
		},
		{
			name:         "with Azure and no fields set",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    "azure-cluster",
			providerSpecValue: &runtime.RawExtension{
				Object: &azure.AzureMachineProviderSpec{},
			},
			expectedError: "[providerSpec.location: Required value: location should be set to one of the supported Azure regions, providerSpec.osDisk.diskSizeGB: Invalid value: 0: diskSizeGB must be greater than zero]",
		},
		{
			name:         "with Azure and a location and disk size set",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    "azure-cluster",
			providerSpecValue: &runtime.RawExtension{
				Object: &azure.AzureMachineProviderSpec{
					Location: "location",
					OSDisk: azure.OSDisk{
						DiskSizeGB: 128,
					},
				},
			},
			expectedError: "",
		},
		{
			name:              "with GCP and a nil provider spec value",
			platformType:      osconfigv1.AWSPlatformType,
			clusterID:         "gcp-cluster",
			providerSpecValue: nil,
			expectedError:     "providerSpec.value: Required value: a value must be provided",
		},
		{
			name:         "with GCP and no fields set",
			platformType: osconfigv1.GCPPlatformType,
			clusterID:    "gcp-cluster",
			providerSpecValue: &runtime.RawExtension{
				Object: &gcp.GCPMachineProviderSpec{},
			},
			expectedError: "providerSpec.region: Required value: region is required",
		},
		{
			name:         "with GCP and the region and zone set",
			platformType: osconfigv1.GCPPlatformType,
			clusterID:    "gcp-cluster",
			providerSpecValue: &runtime.RawExtension{
				Object: &gcp.GCPMachineProviderSpec{
					Region: "region",
					Zone:   "region-zone",
				},
			},
			expectedError: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gs := NewWithT(t)

			mgr, err := manager.New(cfg, manager.Options{
				MetricsBindAddress: "0",
				Port:               testEnv.WebhookInstallOptions.LocalServingPort,
				CertDir:            testEnv.WebhookInstallOptions.LocalServingCertDir,
			})
			gs.Expect(err).ToNot(HaveOccurred())

			done := make(chan struct{})
			go func() {
				gs.Expect(mgr.Start(done)).To(Succeed())
			}()
			defer close(done)

			infra.Status = osconfigv1.InfrastructureStatus{
				InfrastructureName: tc.clusterID,
				PlatformStatus: &osconfigv1.PlatformStatus{
					Type: tc.platformType,
					GCP: &osconfigv1.GCPPlatformStatus{
						ProjectID: "gcp-project-id",
					},
				},
			}
			gs.Expect(c.Status().Update(ctx, infra)).To(Succeed())

			machineSetDefaulter, err := NewMachineSetDefaulter()
			gs.Expect(err).ToNot(HaveOccurred())
			machineSetValidator, err := NewMachineSetValidator()
			gs.Expect(err).ToNot(HaveOccurred())
			mgr.GetWebhookServer().Register("/mutate-machine-openshift-io-v1beta1-machineset", &webhook.Admission{Handler: machineSetDefaulter})
			mgr.GetWebhookServer().Register("/validate-machine-openshift-io-v1beta1-machineset", &webhook.Admission{Handler: machineSetValidator})

			ms := &MachineSet{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "machineset-creation-",
					Namespace:    namespace.Name,
				},
				Spec: MachineSetSpec{
					Template: MachineTemplateSpec{
						Spec: MachineSpec{
							ProviderSpec: ProviderSpec{
								Value: tc.providerSpecValue,
							},
						},
					},
				},
			}
			err = c.Create(ctx, ms)
			if err == nil {
				defer func() {
					gs.Expect(c.Delete(ctx, ms)).To(Succeed())
				}()
			}

			if tc.expectedError != "" {
				gs.Expect(err).ToNot(BeNil())
				gs.Expect(apierrors.ReasonForError(err)).To(BeEquivalentTo(tc.expectedError))
			} else {
				gs.Expect(err).To(BeNil())
			}
		})
	}
}

func TestMachineSetUpdate(t *testing.T) {
	awsClusterID := "aws-cluster"
	defaultAWSProviderSpec := &aws.AWSMachineProviderConfig{
		AMI: aws.AWSResourceReference{
			ID: pointer.StringPtr("ami"),
		},
		InstanceType: defaultAWSInstanceType,
		IAMInstanceProfile: &aws.AWSResourceReference{
			ID: defaultAWSIAMInstanceProfile(awsClusterID),
		},
		UserDataSecret:    &corev1.LocalObjectReference{Name: defaultUserDataSecret},
		CredentialsSecret: &corev1.LocalObjectReference{Name: defaultAWSCredentialsSecret},
		SecurityGroups: []aws.AWSResourceReference{
			{
				Filters: []aws.Filter{
					{
						Name:   "tag:Name",
						Values: []string{defaultAWSSecurityGroup(awsClusterID)},
					},
				},
			},
		},
		Placement: aws.Placement{
			Region:           "region",
			AvailabilityZone: "zone",
		},
		Subnet: aws.AWSResourceReference{
			Filters: []aws.Filter{
				{
					Name:   "tag:Name",
					Values: []string{defaultAWSSubnet(awsClusterID, "zone")},
				},
			},
		},
	}

	azureClusterID := "azure-cluster"
	defaultAzureProviderSpec := &azure.AzureMachineProviderSpec{
		Location:             "location",
		VMSize:               defaultAzureVMSize,
		Vnet:                 defaultAzureVnet(azureClusterID),
		Subnet:               defaultAzureSubnet(azureClusterID),
		NetworkResourceGroup: defaultAzureNetworkResourceGroup(azureClusterID),
		Image: azure.Image{
			ResourceID: defaultAzureImageResourceID(azureClusterID),
		},
		ManagedIdentity: defaultAzureManagedIdentiy(azureClusterID),
		ResourceGroup:   defaultAzureResourceGroup(azureClusterID),
		UserDataSecret: &corev1.SecretReference{
			Name:      defaultUserDataSecret,
			Namespace: defaultSecretNamespace,
		},
		CredentialsSecret: &corev1.SecretReference{
			Name:      defaultAzureCredentialsSecret,
			Namespace: defaultSecretNamespace,
		},
		OSDisk: azure.OSDisk{
			DiskSizeGB: 128,
			OSType:     defaultAzureOSDiskOSType,
			ManagedDisk: azure.ManagedDisk{
				StorageAccountType: defaultAzureOSDiskStorageType,
			},
		},
	}

	gcpClusterID := "gcp-cluster"
	gcpProjectID := "gcp-project-id"
	defaultGCPProviderSpec := &gcp.GCPMachineProviderSpec{
		Region:      "region",
		Zone:        "region-zone",
		MachineType: defaultGCPMachineType,
		NetworkInterfaces: []*gcp.GCPNetworkInterface{
			{
				Network:    defaultGCPNetwork(gcpClusterID),
				Subnetwork: defaultGCPSubnetwork(gcpClusterID),
			},
		},
		Disks: []*gcp.GCPDisk{
			{
				AutoDelete: true,
				Boot:       true,
				SizeGb:     defaultGCPDiskSizeGb,
				Type:       defaultGCPDiskType,
				Image:      defaultGCPDiskImage(gcpClusterID),
			},
		},
		ServiceAccounts: defaultGCPServiceAccounts(gcpClusterID, gcpProjectID),
		Tags:            defaultGCPTags(gcpClusterID),
		UserDataSecret: &corev1.LocalObjectReference{
			Name: defaultUserDataSecret,
		},
		CredentialsSecret: &corev1.LocalObjectReference{
			Name: defaultGCPCredentialsSecret,
		},
	}

	g := NewWithT(t)

	// Override config getter
	ctrl.GetConfig = func() (*rest.Config, error) {
		return cfg, nil
	}
	defer func() {
		ctrl.GetConfig = config.GetConfig
	}()

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "machineset-update-test",
		},
	}
	g.Expect(c.Create(ctx, namespace)).To(Succeed())
	defer func() {
		g.Expect(c.Delete(ctx, namespace)).To(Succeed())
	}()

	infra := &osconfigv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
	}
	g.Expect(c.Create(ctx, infra)).To(Succeed())
	defer func() {
		g.Expect(c.Delete(ctx, infra)).To(Succeed())
	}()

	testCases := []struct {
		name                     string
		platformType             osconfigv1.PlatformType
		clusterID                string
		expectedError            string
		baseProviderSpecValue    *runtime.RawExtension
		updatedProviderSpecValue func() *runtime.RawExtension
	}{
		{
			name:         "with a valid AWS ProviderSpec",
			platformType: osconfigv1.AWSPlatformType,
			clusterID:    awsClusterID,
			baseProviderSpecValue: &runtime.RawExtension{
				Object: defaultAWSProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *runtime.RawExtension {
				return &runtime.RawExtension{
					Object: defaultAWSProviderSpec.DeepCopy(),
				}
			},
			expectedError: "",
		},
		{
			name:         "with an AWS ProviderSpec, removing the instance type",
			platformType: osconfigv1.AWSPlatformType,
			clusterID:    awsClusterID,
			baseProviderSpecValue: &runtime.RawExtension{
				Object: defaultAWSProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *runtime.RawExtension {
				object := defaultAWSProviderSpec.DeepCopy()
				object.InstanceType = ""
				return &runtime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.instanceType: Required value: expected providerSpec.instanceType to be populated",
		},
		{
			name:         "with an AWS ProviderSpec, removing the instance profile",
			platformType: osconfigv1.AWSPlatformType,
			clusterID:    awsClusterID,
			baseProviderSpecValue: &runtime.RawExtension{
				Object: defaultAWSProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *runtime.RawExtension {
				object := defaultAWSProviderSpec.DeepCopy()
				object.IAMInstanceProfile = nil
				return &runtime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.iamInstanceProfile: Required value: expected providerSpec.iamInstanceProfile to be populated",
		},
		{
			name:         "with an AWS ProviderSpec, removing the user data secret",
			platformType: osconfigv1.AWSPlatformType,
			clusterID:    awsClusterID,
			baseProviderSpecValue: &runtime.RawExtension{
				Object: defaultAWSProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *runtime.RawExtension {
				object := defaultAWSProviderSpec.DeepCopy()
				object.UserDataSecret = nil
				return &runtime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.userDataSecret: Required value: expected providerSpec.userDataSecret to be populated",
		},
		{
			name:         "with a valid Azure ProviderSpec",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    azureClusterID,
			baseProviderSpecValue: &runtime.RawExtension{
				Object: defaultAzureProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *runtime.RawExtension {
				return &runtime.RawExtension{
					Object: defaultAzureProviderSpec.DeepCopy(),
				}
			},
			expectedError: "",
		},
		{
			name:         "with an Azure ProviderSpec, removing the vm size",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    azureClusterID,
			baseProviderSpecValue: &runtime.RawExtension{
				Object: defaultAzureProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *runtime.RawExtension {
				object := defaultAzureProviderSpec.DeepCopy()
				object.VMSize = ""
				return &runtime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.vmSize: Required value: vmSize should be set to one of the supported Azure VM sizes",
		},
		{
			name:         "with an Azure ProviderSpec, removing the subnet",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    azureClusterID,
			baseProviderSpecValue: &runtime.RawExtension{
				Object: defaultAzureProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *runtime.RawExtension {
				object := defaultAzureProviderSpec.DeepCopy()
				object.Subnet = ""
				return &runtime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.subnet: Required value: must provide a subnet when a virtual network is specified",
		},
		{
			name:         "with an Azure ProviderSpec, removing the credentials secret",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    azureClusterID,
			baseProviderSpecValue: &runtime.RawExtension{
				Object: defaultAzureProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *runtime.RawExtension {
				object := defaultAzureProviderSpec.DeepCopy()
				object.CredentialsSecret = nil
				return &runtime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.credentialsSecret: Required value: credentialsSecret must be provided",
		},
		{
			name:         "with a valid GCP ProviderSpec",
			platformType: osconfigv1.GCPPlatformType,
			clusterID:    gcpClusterID,
			baseProviderSpecValue: &runtime.RawExtension{
				Object: defaultGCPProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *runtime.RawExtension {
				return &runtime.RawExtension{
					Object: defaultGCPProviderSpec.DeepCopy(),
				}
			},
			expectedError: "",
		},
		{
			name:         "with a GCP ProviderSpec, removing the region",
			platformType: osconfigv1.GCPPlatformType,
			clusterID:    gcpClusterID,
			baseProviderSpecValue: &runtime.RawExtension{
				Object: defaultGCPProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *runtime.RawExtension {
				object := defaultGCPProviderSpec.DeepCopy()
				object.Region = ""
				return &runtime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.region: Required value: region is required",
		},
		{
			name:         "with a GCP ProviderSpec, and an invalid region",
			platformType: osconfigv1.GCPPlatformType,
			clusterID:    gcpClusterID,
			baseProviderSpecValue: &runtime.RawExtension{
				Object: defaultGCPProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *runtime.RawExtension {
				object := defaultGCPProviderSpec.DeepCopy()
				object.Zone = "zone"
				return &runtime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.zone: Invalid value: \"zone\": zone not in configured region (region)",
		},
		{
			name:         "with a GCP ProviderSpec, removing the disks",
			platformType: osconfigv1.GCPPlatformType,
			clusterID:    gcpClusterID,
			baseProviderSpecValue: &runtime.RawExtension{
				Object: defaultGCPProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *runtime.RawExtension {
				object := defaultGCPProviderSpec.DeepCopy()
				object.Disks = nil
				return &runtime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.disks: Required value: at least 1 disk is required",
		},
		{
			name:         "with a GCP ProviderSpec, removing the service accounts",
			platformType: osconfigv1.GCPPlatformType,
			clusterID:    gcpClusterID,
			baseProviderSpecValue: &runtime.RawExtension{
				Object: defaultGCPProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *runtime.RawExtension {
				object := defaultGCPProviderSpec.DeepCopy()
				object.ServiceAccounts = nil
				return &runtime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.serviceAccounts: Invalid value: \"0 service accounts supplied\": exactly 1 service account must be supplied",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gs := NewWithT(t)

			mgr, err := manager.New(cfg, manager.Options{
				MetricsBindAddress: "0",
				Port:               testEnv.WebhookInstallOptions.LocalServingPort,
				CertDir:            testEnv.WebhookInstallOptions.LocalServingCertDir,
			})
			gs.Expect(err).ToNot(HaveOccurred())

			done := make(chan struct{})
			go func() {
				gs.Expect(mgr.Start(done)).To(Succeed())
			}()
			defer close(done)

			infra.Status = osconfigv1.InfrastructureStatus{
				InfrastructureName: tc.clusterID,
				PlatformStatus: &osconfigv1.PlatformStatus{
					Type: tc.platformType,
					GCP: &osconfigv1.GCPPlatformStatus{
						ProjectID: gcpProjectID,
					},
				},
			}
			gs.Expect(c.Status().Update(ctx, infra)).To(Succeed())

			machineSetDefaulter, err := NewMachineSetDefaulter()
			gs.Expect(err).ToNot(HaveOccurred())
			machineSetValidator, err := NewMachineSetValidator()
			gs.Expect(err).ToNot(HaveOccurred())
			mgr.GetWebhookServer().Register("/mutate-machine-openshift-io-v1beta1-machineset", &webhook.Admission{Handler: machineSetDefaulter})
			mgr.GetWebhookServer().Register("/validate-machine-openshift-io-v1beta1-machineset", &webhook.Admission{Handler: machineSetValidator})

			ms := &MachineSet{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "machineset-update-",
					Namespace:    namespace.Name,
				},
				Spec: MachineSetSpec{
					Template: MachineTemplateSpec{
						Spec: MachineSpec{
							ProviderSpec: ProviderSpec{
								Value: tc.baseProviderSpecValue,
							},
						},
					},
				},
			}
			err = c.Create(ctx, ms)
			gs.Expect(err).ToNot(HaveOccurred())
			defer func() {
				gs.Expect(c.Delete(ctx, ms)).To(Succeed())
			}()

			ms.Spec.Template.Spec.ProviderSpec.Value = tc.updatedProviderSpecValue()
			err = c.Update(ctx, ms)
			if tc.expectedError != "" {
				gs.Expect(err).ToNot(BeNil())
				gs.Expect(apierrors.ReasonForError(err)).To(BeEquivalentTo(tc.expectedError))
			} else {
				gs.Expect(err).To(BeNil())
			}
		})
	}
}
