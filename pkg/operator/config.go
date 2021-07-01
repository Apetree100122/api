package operator

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/url"
	"path/filepath"

	configv1 "github.com/openshift/api/config/v1"
)

type Provider string
type NetworkStackType int

const (
	// TODO(alberto): move to "quay.io/openshift/origin-kubemark-machine-controllers:v4.0.0" once available
	clusterAPIControllerKubemark                  = "docker.io/gofed/kubemark-machine-controllers:v1.0"
	clusterAPIControllerNoOp                      = "no-op"
	kubemarkPlatform                              = configv1.PlatformType("kubemark")
	NetworkStackV4               NetworkStackType = 1 << iota
	NetworkStackV6               NetworkStackType = 1 << iota
	NetworkStackDual             NetworkStackType = (NetworkStackV4 | NetworkStackV6)
)

// OperatorConfig contains configuration for MAO
type OperatorConfig struct {
	TargetNamespace      string `json:"targetNamespace"`
	Controllers          Controllers
	BaremetalControllers BaremetalControllers
	Proxy                *configv1.Proxy
	NetworkStack         NetworkStackType
}

type Controllers struct {
	Provider           string
	MachineSet         string
	NodeLink           string
	MachineHealthCheck string
	KubeRBACProxy      string
	TerminationHandler string
}

type BaremetalControllers struct {
	BaremetalOperator         string
	Ironic                    string
	IronicInspector           string
	IronicIpaDownloader       string
	IronicMachineOsDownloader string
	IronicStaticIpManager     string
}

// Images allows build systems to inject images for MAO components
type Images struct {
	MachineAPIOperator            string `json:"machineAPIOperator"`
	ClusterAPIControllerAWS       string `json:"clusterAPIControllerAWS"`
	ClusterAPIControllerOpenStack string `json:"clusterAPIControllerOpenStack"`
	ClusterAPIControllerLibvirt   string `json:"clusterAPIControllerLibvirt"`
	ClusterAPIControllerBareMetal string `json:"clusterAPIControllerBareMetal"`
	ClusterAPIControllerAzure     string `json:"clusterAPIControllerAzure"`
	ClusterAPIControllerGCP       string `json:"clusterAPIControllerGCP"`
	ClusterAPIControllerOvirt     string `json:"clusterAPIControllerOvirt"`
	ClusterAPIControllerVSphere   string `json:"clusterAPIControllerVSphere"`
	KubeRBACProxy                 string `json:"kubeRBACProxy"`
	// Images required for the metal3 pod
	BaremetalOperator            string `json:"baremetalOperator"`
	BaremetalIronic              string `json:"baremetalIronic"`
	BaremetalIronicInspector     string `json:"baremetalIronicInspector"`
	BaremetalIpaDownloader       string `json:"baremetalIpaDownloader"`
	BaremetalMachineOsDownloader string `json:"baremetalMachineOsDownloader"`
	BaremetalStaticIpManager     string `json:"baremetalStaticIpManager"`
}

func networkStack(ips []net.IP) NetworkStackType {
	ns := NetworkStackType(0)
	for _, ip := range ips {
		if ip.IsLoopback() {
			continue
		}
		if ip.To4() != nil {
			ns |= NetworkStackV4
		} else {
			ns |= NetworkStackV6
		}
	}
	return ns
}

func apiServerInternalHost(infra *configv1.Infrastructure) (string, error) {
	if infra.Status.APIServerInternalURL == "" {
		return "", fmt.Errorf("invalid APIServerInternalURL in Infrastructure CR")
	}

	apiServerInternalURL, err := url.Parse(infra.Status.APIServerInternalURL)
	if err != nil {
		return "", fmt.Errorf("unable to parse API Server Internal URL: %w", err)
	}

	host, _, err := net.SplitHostPort(apiServerInternalURL.Host)
	if err != nil {
		return "", err
	}

	return host, nil
}

func getProviderFromInfrastructure(infra *configv1.Infrastructure) (configv1.PlatformType, error) {
	if infra.Status.Platform == "" {
		return "", fmt.Errorf("no platform provider found on install config")
	}
	return infra.Status.Platform, nil
}

func getImagesFromJSONFile(filePath string) (*Images, error) {
	data, err := ioutil.ReadFile(filepath.Clean(filePath))
	if err != nil {
		return nil, err
	}

	var i Images
	if err := json.Unmarshal(data, &i); err != nil {
		return nil, err
	}
	return &i, nil
}

func getProviderControllerFromImages(platform configv1.PlatformType, images Images) (string, error) {
	switch platform {
	case configv1.AWSPlatformType:
		return images.ClusterAPIControllerAWS, nil
	case configv1.LibvirtPlatformType:
		return images.ClusterAPIControllerLibvirt, nil
	case configv1.OpenStackPlatformType:
		return images.ClusterAPIControllerOpenStack, nil
	case configv1.AzurePlatformType:
		return images.ClusterAPIControllerAzure, nil
	case configv1.GCPPlatformType:
		return images.ClusterAPIControllerGCP, nil
	case configv1.BareMetalPlatformType:
		return images.ClusterAPIControllerBareMetal, nil
	case configv1.OvirtPlatformType:
		return images.ClusterAPIControllerOvirt, nil
	case configv1.VSpherePlatformType:
		return images.ClusterAPIControllerVSphere, nil
	case kubemarkPlatform:
		return clusterAPIControllerKubemark, nil
	default:
		return clusterAPIControllerNoOp, nil
	}
}

// getTerminationHandlerFromImages returns the image to use for the Termination Handler DaemonSet
// based on the platform provided.
// Defaults to NoOp if not supported by the platform.
func getTerminationHandlerFromImages(platform configv1.PlatformType, images Images) (string, error) {
	switch platform {
	case configv1.AWSPlatformType:
		return images.ClusterAPIControllerAWS, nil
	case configv1.GCPPlatformType:
		return images.ClusterAPIControllerGCP, nil
	case configv1.AzurePlatformType:
		return images.ClusterAPIControllerAzure, nil
	default:
		return clusterAPIControllerNoOp, nil
	}
}

// This function returns images required to bring up the Baremetal Pod.
func newBaremetalControllers(images Images, usingBareMetal bool) BaremetalControllers {
	if !usingBareMetal {
		return BaremetalControllers{}
	}
	return BaremetalControllers{
		BaremetalOperator:         images.BaremetalOperator,
		Ironic:                    images.BaremetalIronic,
		IronicInspector:           images.BaremetalIronicInspector,
		IronicIpaDownloader:       images.BaremetalIpaDownloader,
		IronicMachineOsDownloader: images.BaremetalMachineOsDownloader,
		IronicStaticIpManager:     images.BaremetalStaticIpManager,
	}
}

func getMachineAPIOperatorFromImages(images Images) (string, error) {
	if images.MachineAPIOperator == "" {
		return "", fmt.Errorf("failed gettingMachineAPIOperator image. It is empty")
	}
	return images.MachineAPIOperator, nil
}

func getKubeRBACProxyFromImages(images Images) (string, error) {
	if images.KubeRBACProxy == "" {
		return "", fmt.Errorf("failed getting kubeRBACProxy image. It is empty")
	}
	return images.KubeRBACProxy, nil
}
