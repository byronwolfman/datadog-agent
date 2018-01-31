// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2018 Datadog, Inc.

// +build kubeapiserver

package leaderelection

import (
	"encoding/json"
	"os"
	"time"

	"github.com/golang/glog"

	ld "k8s.io/client-go/tools/leaderelection"
	rl "k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/api/core/v1"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/api/errors"

	"k8s.io/apimachinery/pkg/util/wait"
)

func getCurrentLeader(electionId, namespace string, c *corev1.CoreV1Client) (string, *v1.Endpoints, error) {
	endpoints, err := c.Endpoints(namespace).Get(electionId, metav1.GetOptions{})

	if err != nil {
		return "", nil, err
	}
	val, found := endpoints.Annotations[rl.LeaderElectionRecordAnnotationKey]
	if !found {
		return "", endpoints, nil
	}

	electionRecord := rl.LeaderElectionRecord{}
	if err := json.Unmarshal([]byte(val), &electionRecord); err != nil {
		return "", nil, err
	}
	return electionRecord.HolderIdentity, endpoints, err
}

// NewElection creates an election.  'namespace'/'election' should be an existing Kubernetes Service
// 'id' is the id if this leader, should be unique.
func NewElection(electionId, id, namespace string, ttl time.Duration, callback func(leader string), c *corev1.CoreV1Client) (*ld.LeaderElector, error) {
	_, err := c.Endpoints(namespace).Get(electionId, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			_, err = c.Endpoints(namespace).Create(&v1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name: electionId,
				},
			})
			if err != nil && !errors.IsConflict(err) {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	leader, endpoints, err := getCurrentLeader(electionId, namespace, c)
	if err != nil {
		return nil, err
	}
	callback(leader)

	hostname, err := os.Hostname()
	if err != nil {
		return nil, err
	}

	eventSource := v1.EventSource{
		Component: "leader-elector",
		Host:      hostname,
	}
	broadcaster := record.NewBroadcaster()

	evRec := broadcaster.NewRecorder(runtime.NewScheme(),eventSource)

	resourceLockConfig := rl.ResourceLockConfig{
		Identity:      hostname,
		EventRecorder: evRec,
	}

	callbacks := ld.LeaderCallbacks{
		OnStartedLeading: func(stop <-chan struct{}) {
			callback(id)
		},
		OnStoppedLeading: func() {
			leader, _, err := getCurrentLeader(electionId, namespace, c)
			if err != nil {
				glog.Errorf("failed to get leader: %v", err)
				// empty string means leader is unknown
				callback("")
				return
			}
			callback(leader)
		},
		OnNewLeader: func(identity string) {
			callback(identity)
		},
	}

	leaderElectorinterface, err := rl.New(rl.EndpointsResourceLock,endpoints.ObjectMeta.Namespace,endpoints.ObjectMeta.Name, c ,resourceLockConfig)
	if err != nil {
		return nil, err
	}

	config := ld.LeaderElectionConfig{
		Lock:          leaderElectorinterface,
		LeaseDuration: ttl,
		RenewDeadline: ttl / 2,
		RetryPeriod:   ttl / 4,
		Callbacks:     callbacks,
	}

	return ld.NewLeaderElector(config)
}

// RunElection runs an election given an leader elector. Doesn't return.
func RunElection(e *ld.LeaderElector) {
	wait.Forever(e.Run, 0)
}