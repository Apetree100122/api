package vsphere

import (
	"fmt"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	testRegion                      = "testRegion"
	testZone                        = "testZone"
	testPort                        = "443"
	testInsecureFlag                = true
	openshiftConfigNamespaceForTest = "openshift-config-test"
	testConfigFmt                   = `
[Labels]
zone = "testZone"
region = "testRegion"
[VirtualCenter "127.0.0.1"]
port = %s
[Global]
insecure-flag="1"
secret-name = "%s"
secret-namespace = "%s"
`
)

func TestGetVSphereConfig(t *testing.T) {
	testConfig := fmt.Sprintf(testConfigFmt, "443", "test", "test-namespace")
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testName",
			Namespace: openshiftConfigNamespaceForTest,
		},
		Data: map[string]string{
			"testKey": testConfig,
		},
	}

	infra := &configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{
			Name: globalInfrastuctureName,
		},
		Spec: configv1.InfrastructureSpec{
			CloudConfig: configv1.ConfigMapFileReference{
				Name: "testName",
				Key:  "testKey",
			},
		},
	}

	client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(infra, configMap).Build()

	vSphereConfig, err := getVSphereConfig(client, openshiftConfigNamespaceForTest)
	if err != nil {
		t.Fatal(err)
	}

	if vSphereConfig.Labels.Region != testRegion {
		t.Errorf("Expected region %s, got %s", testRegion, vSphereConfig.Labels.Region)
	}

	if vSphereConfig.Labels.Zone != testZone {
		t.Errorf("Expected zone %s, got %s", testZone, vSphereConfig.Labels.Zone)
	}

	if vSphereConfig.Global.VCenterPort != testPort {
		t.Errorf("Expected zone %s, got %s", testZone, vSphereConfig.Global.VCenterPort)
	}

	if vSphereConfig.Global.InsecureFlag != testInsecureFlag {
		t.Errorf("Expected insecure flag %t, got %t", testInsecureFlag, vSphereConfig.Global.InsecureFlag)
	}
}
