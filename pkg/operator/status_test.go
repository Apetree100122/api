package operator

import (
	"reflect"
	"testing"
	"time"

	osconfigv1 "github.com/openshift/api/config/v1"
	fakeconfigclientset "github.com/openshift/client-go/config/clientset/versioned/fake"
	cvoresourcemerge "github.com/openshift/cluster-version-operator/lib/resourcemerge"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
)

func TestPrintOperandVersions(t *testing.T) {
	optr := Operator{
		operandVersions: []osconfigv1.OperandVersion{
			{
				Name:    "operator",
				Version: "1.0",
			},
			{
				Name:    "controller-manager",
				Version: "2.0",
			},
		},
	}
	expectedOutput := "operator: 1.0, controller-manager: 2.0"
	got := optr.printOperandVersions()
	if got != expectedOutput {
		t.Errorf("Expected: %s, got: %s", expectedOutput, got)
	}
}

func TestOperatorStatusProgressing(t *testing.T) {
	type tCase struct {
		currentVersion        []osconfigv1.OperandVersion
		desiredVersion        []osconfigv1.OperandVersion
		expectedConditions    []osconfigv1.ClusterOperatorStatusCondition
		transitionTimeUpdated bool
	}
	tCases := []tCase{
		{
			currentVersion: []osconfigv1.OperandVersion{
				{
					Name:    "operator",
					Version: "1.0",
				},
			},
			desiredVersion: []osconfigv1.OperandVersion{
				{
					Name:    "operator",
					Version: "1.0",
				},
			},
			expectedConditions: []osconfigv1.ClusterOperatorStatusCondition{
				newClusterOperatorStatusCondition(osconfigv1.OperatorProgressing, osconfigv1.ConditionFalse, string(ReasonSyncing), ""),
				newClusterOperatorStatusCondition(osconfigv1.OperatorAvailable, osconfigv1.ConditionTrue, "", ""),
				newClusterOperatorStatusCondition(osconfigv1.OperatorDegraded, osconfigv1.ConditionFalse, "", ""),
				operatorUpgradeable,
			},
		},
		{
			currentVersion: []osconfigv1.OperandVersion{
				{
					Name:    "operator",
					Version: "1.0",
				},
			},
			desiredVersion: []osconfigv1.OperandVersion{
				{
					Name:    "operator",
					Version: "2.0",
				},
			},
			expectedConditions: []osconfigv1.ClusterOperatorStatusCondition{
				newClusterOperatorStatusCondition(osconfigv1.OperatorProgressing, osconfigv1.ConditionTrue, string(ReasonSyncing), ""),
				newClusterOperatorStatusCondition(osconfigv1.OperatorAvailable, osconfigv1.ConditionTrue, "", ""),
				newClusterOperatorStatusCondition(osconfigv1.OperatorDegraded, osconfigv1.ConditionFalse, "", ""),
				operatorUpgradeable,
			},
		},
	}

	optr := Operator{eventRecorder: record.NewFakeRecorder(5)}
	for i, tc := range tCases {
		optr.operandVersions = tc.currentVersion
		co := &osconfigv1.ClusterOperator{ObjectMeta: metav1.ObjectMeta{Name: clusterOperatorName}}
		co.Status.Versions = tc.desiredVersion

		optr.osClient = fakeconfigclientset.NewSimpleClientset(co)
		startTime := metav1.Now()
		optr.statusProgressing()
		gotCO, _ := optr.osClient.ConfigV1().ClusterOperators().Get(clusterOperatorName, metav1.GetOptions{})
		var condition osconfigv1.ClusterOperatorStatusCondition
		for _, coCondition := range gotCO.Status.Conditions {
			assert.True(t, startTime.Before(&coCondition.LastTransitionTime), "test-case %v expected LastTransitionTime for the status condition to be updated", i)
			if coCondition.Type == osconfigv1.OperatorProgressing {
				condition = coCondition
				break
			}
		}

		for _, expectedCondition := range tc.expectedConditions {
			ok := cvoresourcemerge.IsOperatorStatusConditionPresentAndEqual(
				gotCO.Status.Conditions, expectedCondition.Type, expectedCondition.Status,
			)
			if !ok {
				t.Errorf("wrong status for condition. Expected: %v, got: %v",
					expectedCondition,
					cvoresourcemerge.FindOperatorStatusCondition(gotCO.Status.Conditions, expectedCondition.Type))
			}
		}

		optr.statusProgressing()
		gotCO, _ = optr.osClient.ConfigV1().ClusterOperators().Get(clusterOperatorName, metav1.GetOptions{})
		var conditionAfterAnotherSync osconfigv1.ClusterOperatorStatusCondition
		for _, coCondition := range gotCO.Status.Conditions {
			if coCondition.Type == osconfigv1.OperatorProgressing {
				conditionAfterAnotherSync = coCondition
				break
			}
		}
		assert.True(t, condition.LastTransitionTime.Equal(&conditionAfterAnotherSync.LastTransitionTime), "test-case %v expected LastTransitionTime not to be updated if condition state is same", i)

		for _, expectedCondition := range tc.expectedConditions {
			ok := cvoresourcemerge.IsOperatorStatusConditionPresentAndEqual(
				gotCO.Status.Conditions, expectedCondition.Type, expectedCondition.Status,
			)
			if !ok {
				t.Errorf("wrong status for condition. Expected: %v, got: %v",
					expectedCondition,
					cvoresourcemerge.FindOperatorStatusCondition(gotCO.Status.Conditions, expectedCondition.Type))
			}
		}
	}
}

func TestGetOrCreateClusterOperator(t *testing.T) {
	var namespace = "some-namespace"
	var conditions = []osconfigv1.ClusterOperatorStatusCondition{
		{
			Type:               "Available",
			Status:             "true",
			Reason:             "",
			Message:            "",
			LastTransitionTime: metav1.NewTime(time.Now()),
		},
	}
	testCases := []struct {
		existingCO *osconfigv1.ClusterOperator
		expectedCO *osconfigv1.ClusterOperator
	}{
		{
			existingCO: nil,
			expectedCO: &osconfigv1.ClusterOperator{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterOperatorName,
				},
				Status: osconfigv1.ClusterOperatorStatus{
					RelatedObjects: []osconfigv1.ObjectReference{
						{
							Group:    "",
							Resource: "namespaces",
							Name:     namespace,
						},
						{
							Group:     "machine.openshift.io",
							Resource:  "machines",
							Name:      "",
							Namespace: namespace,
						},
						{
							Group:     "machine.openshift.io",
							Resource:  "machinesets",
							Name:      "",
							Namespace: namespace,
						},
						{
							Group:     "metal3.io",
							Resource:  "baremetalhosts",
							Name:      "",
							Namespace: namespace,
						},
					},
				},
			},
		},
		{
			existingCO: &osconfigv1.ClusterOperator{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterOperatorName,
				},
				Status: osconfigv1.ClusterOperatorStatus{
					Conditions: conditions,
				},
			},
			expectedCO: &osconfigv1.ClusterOperator{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterOperatorName,
				},
				Status: osconfigv1.ClusterOperatorStatus{
					RelatedObjects: []osconfigv1.ObjectReference{
						{
							Group:    "",
							Resource: "namespaces",
							Name:     namespace,
						},
						{
							Group:     "machine.openshift.io",
							Resource:  "machines",
							Name:      "",
							Namespace: namespace,
						},
						{
							Group:     "machine.openshift.io",
							Resource:  "machinesets",
							Name:      "",
							Namespace: namespace,
						},
						{
							Group:     "metal3.io",
							Resource:  "baremetalhosts",
							Name:      "",
							Namespace: namespace,
						},
					},
					Conditions: conditions,
				},
			},
		},
	}

	for _, tc := range testCases {
		var osClient *fakeconfigclientset.Clientset
		if tc.existingCO != nil {
			osClient = fakeconfigclientset.NewSimpleClientset(tc.existingCO)
		} else {
			osClient = fakeconfigclientset.NewSimpleClientset()
		}
		optr := Operator{
			osClient:  osClient,
			namespace: namespace,
		}

		co, err := optr.getOrCreateClusterOperator()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !reflect.DeepEqual(co, tc.expectedCO) {
			t.Errorf("got: %v, expected: %v", co, tc.expectedCO)
		}
	}
}
