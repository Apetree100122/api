package machinehealthcheck

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"sigs.k8s.io/controller-runtime/pkg/handler"

	mapiv1beta1 "github.com/openshift/cluster-api/pkg/apis/machine/v1beta1"
	healthcheckingv1alpha1 "github.com/openshift/machine-api-operator/pkg/apis/healthchecking/v1alpha1"
	"github.com/openshift/machine-api-operator/pkg/util/conditions"
	maotesting "github.com/openshift/machine-api-operator/pkg/util/testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	namespace         = "openshift-machine-api"
	badConditionsData = `items:
- name: Ready 
  timeout: 60s
  status: Unknown`
)

func init() {
	// Add types to scheme
	mapiv1beta1.AddToScheme(scheme.Scheme)
	healthcheckingv1alpha1.AddToScheme(scheme.Scheme)
}

func TestHasMatchingLabels(t *testing.T) {
	machine := maotesting.NewMachine("machine", "node")
	testsCases := []struct {
		machine            *mapiv1beta1.Machine
		machineHealthCheck *healthcheckingv1alpha1.MachineHealthCheck
		expected           bool
	}{
		{
			machine:            machine,
			machineHealthCheck: maotesting.NewMachineHealthCheck("foobar"),
			expected:           true,
		},
		{
			machine: machine,
			machineHealthCheck: &healthcheckingv1alpha1.MachineHealthCheck{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "NoMatchingLabels",
					Namespace: namespace,
				},
				TypeMeta: metav1.TypeMeta{
					Kind: "MachineHealthCheck",
				},
				Spec: healthcheckingv1alpha1.MachineHealthCheckSpec{
					Selector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"no": "match",
						},
					},
				},
				Status: healthcheckingv1alpha1.MachineHealthCheckStatus{},
			},
			expected: false,
		},
	}

	for _, tc := range testsCases {
		if got := hasMatchingLabels(tc.machineHealthCheck, tc.machine); got != tc.expected {
			t.Errorf("Test case: %s. Expected: %t, got: %t", tc.machineHealthCheck.Name, tc.expected, got)
		}
	}
}

func TestGetNodeCondition(t *testing.T) {

	testsCases := []struct {
		node      *corev1.Node
		condition *corev1.NodeCondition
		expected  *corev1.NodeCondition
	}{
		{
			node: maotesting.NewNode("hasCondition", true),
			condition: &corev1.NodeCondition{
				Type:   corev1.NodeReady,
				Status: corev1.ConditionTrue,
			},
			expected: &corev1.NodeCondition{
				Type:               corev1.NodeReady,
				Status:             corev1.ConditionTrue,
				LastTransitionTime: maotesting.KnownDate,
			},
		},
		{
			node: maotesting.NewNode("doesNotHaveCondition", true),
			condition: &corev1.NodeCondition{
				Type:   corev1.NodeOutOfDisk,
				Status: corev1.ConditionTrue,
			},
			expected: nil,
		},
	}

	for _, tc := range testsCases {
		got := conditions.GetNodeCondition(tc.node, tc.condition.Type)
		if !reflect.DeepEqual(got, tc.expected) {
			t.Errorf("Test case: %s. Expected: %v, got: %v", tc.node.Name, tc.expected, got)
		}
	}

}

// newFakeReconciler returns a new reconcile.Reconciler with a fake client
func newFakeReconciler(initObjects ...runtime.Object) *ReconcileMachineHealthCheck {
	fakeClient := fake.NewFakeClient(initObjects...)
	return &ReconcileMachineHealthCheck{
		client:    fakeClient,
		scheme:    scheme.Scheme,
		namespace: namespace,
	}
}

type expectedReconcile struct {
	result reconcile.Result
	error  bool
}

func testReconcile(t *testing.T, remediationWaitTime time.Duration, initObjects ...runtime.Object) {
	// healthy node
	nodeHealthy := maotesting.NewNode("healthy", true)
	nodeHealthy.Annotations = map[string]string{
		machineAnnotationKey: fmt.Sprintf("%s/%s", namespace, "machineWithNodehealthy"),
	}
	machineWithNodeHealthy := maotesting.NewMachine("machineWithNodehealthy", nodeHealthy.Name)

	// recently unhealthy node
	nodeRecentlyUnhealthy := maotesting.NewNode("recentlyUnhealthy", false)
	nodeRecentlyUnhealthy.Status.Conditions[0].LastTransitionTime = metav1.Time{Time: time.Now()}
	nodeRecentlyUnhealthy.Annotations = map[string]string{
		machineAnnotationKey: fmt.Sprintf("%s/%s", namespace, "machineWithNodeRecentlyUnhealthy"),
	}
	machineWithNodeRecentlyUnhealthy := maotesting.NewMachine("machineWithNodeRecentlyUnhealthy", nodeRecentlyUnhealthy.Name)

	// node without machine annotation
	nodeWithoutMachineAnnotation := maotesting.NewNode("withoutMachineAnnotation", true)
	nodeWithoutMachineAnnotation.Annotations = map[string]string{}

	// node annotated with machine that does not exist
	nodeAnnotatedWithNoExistentMachine := maotesting.NewNode("annotatedWithNoExistentMachine", true)
	nodeAnnotatedWithNoExistentMachine.Annotations[machineAnnotationKey] = "annotatedWithNoExistentMachine"

	// node annotated with machine without owner reference
	nodeAnnotatedWithMachineWithoutOwnerReference := maotesting.NewNode("annotatedWithMachineWithoutOwnerReference", true)
	nodeAnnotatedWithMachineWithoutOwnerReference.Annotations = map[string]string{
		machineAnnotationKey: fmt.Sprintf("%s/%s", namespace, "machineWithoutOwnerController"),
	}
	machineWithoutOwnerController := maotesting.NewMachine("machineWithoutOwnerController", nodeAnnotatedWithMachineWithoutOwnerReference.Name)
	machineWithoutOwnerController.OwnerReferences = nil

	// node annotated with machine without node reference
	nodeAnnotatedWithMachineWithoutNodeReference := maotesting.NewNode("annotatedWithMachineWithoutNodeReference", true)
	nodeAnnotatedWithMachineWithoutNodeReference.Annotations = map[string]string{
		machineAnnotationKey: fmt.Sprintf("%s/%s", namespace, "machineWithoutNodeRef"),
	}
	machineWithoutNodeRef := maotesting.NewMachine("machineWithoutNodeRef", nodeAnnotatedWithMachineWithoutNodeReference.Name)
	machineWithoutNodeRef.Status.NodeRef = nil

	machineHealthCheck := maotesting.NewMachineHealthCheck("machineHealthCheck")

	// remediationReboot
	nodeUnhealthyForTooLong := maotesting.NewNode("nodeUnhealthyForTooLong", false)
	nodeUnhealthyForTooLong.Annotations = map[string]string{
		machineAnnotationKey: fmt.Sprintf("%s/%s", namespace, "machineUnhealthyForTooLong"),
	}
	machineUnhealthyForTooLong := maotesting.NewMachine("machineUnhealthyForTooLong", nodeUnhealthyForTooLong.Name)

	testsCases := []struct {
		machine             *mapiv1beta1.Machine
		node                *corev1.Node
		remediationStrategy healthcheckingv1alpha1.RemediationStrategyType
		expected            expectedReconcile
	}{
		{
			machine: machineUnhealthyForTooLong,
			node:    nodeUnhealthyForTooLong,
			expected: expectedReconcile{
				result: reconcile.Result{},
				error:  false,
			},
			remediationStrategy: remediationStrategyReboot,
		},
		{
			machine: machineWithNodeHealthy,
			node:    nodeHealthy,
			expected: expectedReconcile{
				result: reconcile.Result{},
				error:  false,
			},
		},
		{
			machine: machineWithNodeRecentlyUnhealthy,
			node:    nodeRecentlyUnhealthy,
			expected: expectedReconcile{
				result: reconcile.Result{
					Requeue:      true,
					RequeueAfter: remediationWaitTime,
				},
				error: false,
			},
		},
		{
			machine: nil,
			node:    nodeWithoutMachineAnnotation,
			expected: expectedReconcile{
				result: reconcile.Result{},
				error:  false,
			},
		},
		{
			machine: nil,
			node:    nodeAnnotatedWithNoExistentMachine,
			expected: expectedReconcile{
				result: reconcile.Result{},
				error:  false,
			},
		},
		{
			machine: machineWithoutOwnerController,
			node:    nodeAnnotatedWithMachineWithoutOwnerReference,
			expected: expectedReconcile{
				result: reconcile.Result{},
				error:  false,
			},
		},
		{
			machine: machineWithoutNodeRef,
			node:    nodeAnnotatedWithMachineWithoutNodeReference,
			expected: expectedReconcile{
				result: reconcile.Result{},
				error:  true,
			},
		},
	}

	for _, tc := range testsCases {
		machineHealthCheck.Spec.RemediationStrategy = &tc.remediationStrategy
		objects := []runtime.Object{}
		objects = append(objects, initObjects...)
		objects = append(objects, machineHealthCheck)
		if tc.machine != nil {
			objects = append(objects, tc.machine)
		}
		objects = append(objects, tc.node)
		r := newFakeReconciler(objects...)

		request := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: "",
				Name:      tc.node.Name,
			},
		}
		result, err := r.Reconcile(request)
		if tc.expected.error != (err != nil) {
			var errorExpectation string
			if !tc.expected.error {
				errorExpectation = "no"
			}
			t.Errorf("Test case: %s. Expected: %s error, got: %v", tc.node.Name, errorExpectation, err)
		}

		if result != tc.expected.result {
			if tc.expected.result.Requeue {
				before := tc.expected.result.RequeueAfter
				after := tc.expected.result.RequeueAfter + time.Second
				if after < result.RequeueAfter || before > result.RequeueAfter {
					t.Errorf("Test case: %s. Expected RequeueAfter between: %v and %v, got: %v", tc.node.Name, before, after, result)
				}
			} else {
				t.Errorf("Test case: %s. Expected: %v, got: %v", tc.node.Name, tc.expected.result, result)
			}
		}
		if tc.remediationStrategy == remediationStrategyReboot {
			node := &corev1.Node{}
			if err := r.client.Get(context.TODO(), request.NamespacedName, node); err != nil {
				t.Errorf("Expected: no error, got: %v", err)
			}

			if _, ok := node.Annotations[machineRebootAnnotationKey]; !ok {
				t.Errorf("Expected: node to have reboot annotion %s, got: %v", machineRebootAnnotationKey, node.Annotations)
			}
		}
	}
}

func TestReconcileWithoutUnhealthyConditionsConfigMap(t *testing.T) {
	testReconcile(t, 5*time.Minute)
}

func TestReconcileWithUnhealthyConditionsConfigMap(t *testing.T) {
	cmBadConditions := maotesting.NewUnhealthyConditionsConfigMap(healthcheckingv1alpha1.ConfigMapNodeUnhealthyConditions, badConditionsData)
	testReconcile(t, 1*time.Minute, cmBadConditions)
}

func TestHasMachineSetOwner(t *testing.T) {
	machineWithMachineSet := maotesting.NewMachine("machineWithMachineSet", "node")
	machineWithNoMachineSet := maotesting.NewMachine("machineWithNoMachineSet", "node")
	machineWithNoMachineSet.OwnerReferences = nil

	testsCases := []struct {
		machine  *mapiv1beta1.Machine
		expected bool
	}{
		{
			machine:  machineWithNoMachineSet,
			expected: false,
		},
		{
			machine:  machineWithMachineSet,
			expected: true,
		},
	}

	for _, tc := range testsCases {
		if got := hasMachineSetOwner(*tc.machine); got != tc.expected {
			t.Errorf("Test case: Machine %s. Expected: %t, got: %t", tc.machine.Name, tc.expected, got)
		}
	}

}

func TestUnhealthyForTooLong(t *testing.T) {
	nodeUnhealthyForTooLong := maotesting.NewNode("nodeUnhealthyForTooLong", false)
	nodeRecentlyUnhealthy := maotesting.NewNode("nodeRecentlyUnhealthy", false)
	nodeRecentlyUnhealthy.Status.Conditions[0].LastTransitionTime = metav1.Time{Time: time.Now()}
	testsCases := []struct {
		node     *corev1.Node
		expected bool
	}{
		{
			node:     nodeUnhealthyForTooLong,
			expected: true,
		},
		{
			node:     nodeRecentlyUnhealthy,
			expected: false,
		},
	}
	for _, tc := range testsCases {
		if got := unhealthyForTooLong(&tc.node.Status.Conditions[0], time.Minute); got != tc.expected {
			t.Errorf("Test case: %s. Expected: %t, got: %t", tc.node.Name, tc.expected, got)
		}
	}
}

func TestApplyRemediationReboot(t *testing.T) {
	nodeUnhealthyForTooLong := maotesting.NewNode("nodeUnhealthyForTooLong", false)
	nodeUnhealthyForTooLong.Annotations = map[string]string{
		machineAnnotationKey: fmt.Sprintf("%s/%s", namespace, "machineUnhealthyForTooLong"),
	}
	machineUnhealthyForTooLong := maotesting.NewMachine("machineUnhealthyForTooLong", nodeUnhealthyForTooLong.Name)
	machineHealthCheck := maotesting.NewMachineHealthCheck("machineHealthCheck")
	request := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Namespace: "",
			Name:      nodeUnhealthyForTooLong.Name,
		},
	}
	r := newFakeReconciler(nodeUnhealthyForTooLong, machineUnhealthyForTooLong, machineHealthCheck)
	_, err := r.remediationStrategyReboot(machineUnhealthyForTooLong, nodeUnhealthyForTooLong)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	node := &corev1.Node{}
	if err := r.client.Get(context.TODO(), request.NamespacedName, node); err != nil {
		t.Errorf("Expected: no error, got: %v", err)
	}

	if _, ok := node.Annotations[machineRebootAnnotationKey]; !ok {
		t.Errorf("Expected: node to have reboot annotion %s, got: %v", machineRebootAnnotationKey, node.Annotations)
	}
}

func TestGetNodeNamesForMHC(t *testing.T) {
	testCases := []struct {
		mhc               healthcheckingv1alpha1.MachineHealthCheck
		machines          []*mapiv1beta1.Machine
		expectedNodeNames []types.NodeName
	}{
		{
			mhc: *maotesting.NewMachineHealthCheck("matchNodes"),
			machines: []*mapiv1beta1.Machine{
				maotesting.NewMachine("test", "node1"),
				maotesting.NewMachine("test2", "node2"),
			},
			expectedNodeNames: []types.NodeName{
				"node1",
				"node2",
			},
		},
		{
			mhc: *&healthcheckingv1alpha1.MachineHealthCheck{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "noMatchingMachines",
					Namespace: namespace,
				},
				TypeMeta: metav1.TypeMeta{
					Kind: "MachineHealthCheck",
				},
				Spec: healthcheckingv1alpha1.MachineHealthCheckSpec{
					Selector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"no": "match",
						},
					},
				},
				Status: healthcheckingv1alpha1.MachineHealthCheckStatus{},
			},
			machines: []*mapiv1beta1.Machine{
				maotesting.NewMachine("test", "node1"),
				maotesting.NewMachine("test2", "node2"),
			},
			expectedNodeNames: nil,
		},
		{
			mhc: *maotesting.NewMachineHealthCheck("noNodeRefs"),
			machines: []*mapiv1beta1.Machine{
				maotesting.NewMachine("test", ""),
				maotesting.NewMachine("test2", ""),
			},
			expectedNodeNames: nil,
		},
	}
	for _, tc := range testCases {
		objects := []runtime.Object{}
		for i := range tc.machines {
			objects = append(objects, runtime.Object(tc.machines[i]))
		}
		r := newFakeReconciler(objects...)
		nodeNames, err := r.getNodeNamesForMHC(tc.mhc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !reflect.DeepEqual(nodeNames, tc.expectedNodeNames) {
			t.Errorf("Expected: %v, got: %v", tc.expectedNodeNames, nodeNames)
		}
	}
}

func TestNodeRequestsFromMachineHealthCheck(t *testing.T) {
	testCases := []struct {
		mhc              healthcheckingv1alpha1.MachineHealthCheck
		machines         []*mapiv1beta1.Machine
		expectedRequests []reconcile.Request
	}{
		{
			mhc: *maotesting.NewMachineHealthCheck("matchNodes"),
			machines: []*mapiv1beta1.Machine{
				maotesting.NewMachine("test", "node1"),
				maotesting.NewMachine("test2", "node2"),
			},
			expectedRequests: []reconcile.Request{
				{
					NamespacedName: client.ObjectKey{
						Name: string("node1"),
					},
				},
				{
					NamespacedName: client.ObjectKey{
						Name: string("node2"),
					},
				},
			},
		},
		{
			mhc: *&healthcheckingv1alpha1.MachineHealthCheck{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "noMatchingMachines",
					Namespace: namespace,
				},
				TypeMeta: metav1.TypeMeta{
					Kind: "MachineHealthCheck",
				},
				Spec: healthcheckingv1alpha1.MachineHealthCheckSpec{
					Selector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"no": "match",
						},
					},
				},
				Status: healthcheckingv1alpha1.MachineHealthCheckStatus{},
			},
			machines: []*mapiv1beta1.Machine{
				maotesting.NewMachine("test", "node1"),
				maotesting.NewMachine("test2", "node2"),
			},
			expectedRequests: []reconcile.Request{},
		},
		{
			mhc: *maotesting.NewMachineHealthCheck("noNodeRefs"),
			machines: []*mapiv1beta1.Machine{
				maotesting.NewMachine("test", ""),
				maotesting.NewMachine("test2", ""),
			},
			expectedRequests: []reconcile.Request{},
		},
	}
	for _, tc := range testCases {
		objects := []runtime.Object{}
		for i := range tc.machines {
			objects = append(objects, runtime.Object(tc.machines[i]))
		}
		objects = append(objects, runtime.Object(&tc.mhc))
		r := newFakeReconciler(objects...)
		o := handler.MapObject{
			Meta:   tc.mhc.GetObjectMeta(),
			Object: &tc.mhc,
		}
		requests := r.nodeRequestsFromMachineHealthCheck(o)
		if !reflect.DeepEqual(requests, tc.expectedRequests) {
			t.Errorf("Expected: %v, got: %v", tc.expectedRequests, requests)
		}
	}
}
