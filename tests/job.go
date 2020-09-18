package tests

import (
	"fmt"
	"strconv"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	k8sv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"kubevirt.io/client-go/kubecli"
)

const (
	toSucceed = true
	toFail    = false
)

// WaitForJobToSucceed blocks until the given job finishes.
// On success, it returns with a nil error, on failure or timeout it returns with an error.
func WaitForJobToSucceed(job *batchv1.Job, timeout time.Duration) error {
	return waitForJob(job, toSucceed, timeout)
}

// WaitForJobToFail blocks until the given job finishes.
// On failure, it returns with a nil error, on success or timeout it returns with an error.
func WaitForJobToFail(job *batchv1.Job, timeout time.Duration) error {
	return waitForJob(job, toFail, timeout)
}

func waitForJob(job *batchv1.Job, toSucceed bool, timeout time.Duration) error {
	virtClient, err := kubecli.GetKubevirtClient()
	if err != nil {
		return err
	}

	jobFailedError := func(job *batchv1.Job) error {
		if toSucceed {
			return fmt.Errorf("Job %s finished with failure, status: %+v", job.Name, job.Status)
		}
		return nil
	}
	jobCompleteError := func(job *batchv1.Job) error {
		if toSucceed {
			return nil
		}
		return fmt.Errorf("Job %s finished with success, status: %+v", job.Name, job.Status)
	}

	const finish = true
	err = wait.PollImmediate(time.Second, timeout, func() (bool, error) {
		job, err = virtClient.BatchV1().Jobs(job.Namespace).Get(job.Name, metav1.GetOptions{})
		if err != nil {
			return finish, err
		}
		for _, c := range job.Status.Conditions {
			switch c.Type {
			case batchv1.JobComplete:
				if c.Status == k8sv1.ConditionTrue {
					return finish, jobCompleteError(job)
				}
			case batchv1.JobFailed:
				if c.Status == k8sv1.ConditionTrue {
					return finish, jobFailedError(job)
				}
			}
		}
		return !finish, nil
	})

	if err != nil {
		return fmt.Errorf("Job %s timeout reached, status: %+v, err: %v", job.Name, job.Status, err)
	}
	return nil
}

// Default Job arguments to be used with NewJob.
const (
	JobRetry   = 3
	JobTTL     = 60
	JobTimeout = 480
)

// NewJob creates a job configuration that runs a single Pod.
// A name is used for the job & pod while the command and its arguments are passed to the pod for execution.
// In addition, the following arguments control the job behavior:
// retry: The number of times the job should try and run the pod.
// ttlAfterFinished: The period of time between the job finishing and its auto-deletion.
//                   Make sure to leave enough time for the reporter to collect the logs.
// timeout: The overall time at which the job is terminated, regardless of it finishing or not.
func NewJob(name string, cmd, args []string, retry, ttlAfterFinished int32, timeout int64) *batchv1.Job {
	pod := RenderPod(name, cmd, args)
	job := batchv1.Job{
		ObjectMeta: pod.ObjectMeta,
		Spec: batchv1.JobSpec{
			BackoffLimit:            &retry,
			TTLSecondsAfterFinished: &ttlAfterFinished,
			ActiveDeadlineSeconds:   &timeout,
			Template: k8sv1.PodTemplateSpec{
				ObjectMeta: pod.ObjectMeta,
				Spec:       pod.Spec,
			},
		},
	}
	return &job
}

// NewHelloWorldJob takes a DNS entry or an IP and a port which it will use to create a job
// which tries to contact the host on the provided port.
// It expects to receive "Hello World!" to succeed.
func NewHelloWorldJob(host string, port string) *batchv1.Job {
	check := []string{fmt.Sprintf(`set -x; ping -c 1 %s; x="$(head -n 1 < <(nc %s %s -i 3 -w 3))"; echo "$x" ; if [ "$x" = "Hello World!" ]; then echo "succeeded"; exit 0; else echo "failed"; exit 1; fi`, host, host, port)}
	job := NewJob("netcat", []string{"/bin/bash", "-c"}, check, JobRetry, JobTTL, JobTimeout)
	return job
}

// NewHelloWorldJobUDP takes a DNS entry or an IP and a port which it will use create a pod
// which tries to contact the host on the provided port. It expects to receive "Hello World!" to succeed.
// Note that in case of UDP, the server will not see the connection unless something is sent over it
// However, netcat does not work well with UDP and closes before the answer arrives, for that another netcat call is needed,
// this time as a UDP listener
func NewHelloWorldJobUDP(host string, port string) *batchv1.Job {
	localPort, err := strconv.Atoi(port)
	if err != nil {
		return nil
	}
	// local port is used to catch the reply - any number can be used
	// we make it different than the port to be safe if both are running on the same machine
	localPort--
	check := []string{fmt.Sprintf(`set -x; trap "kill 0" EXIT; x="$(head -n 1 < <(echo | nc -up %d %s %s -i 3 -w 3 & nc -ul %d))"; echo "$x" ; if [ "$x" = "Hello UDP World!" ]; then echo "succeeded"; exit 0; else echo "failed"; exit 1; fi`,
		localPort, host, port, localPort)}
	job := NewJob("netcat", []string{"/bin/bash", "-c"}, check, JobRetry, JobTTL, JobTimeout)

	return job
}

// NewHelloWorldJobHTTP gets an IP address and a port, which it uses to create a pod.
// This pod tries to contact the host on the provided port, over HTTP.
// On success - it expects to receive "Hello World!".
func NewHelloWorldJobHTTP(host string, port string) *batchv1.Job {
	check := []string{fmt.Sprintf(`set -x; x="$(head -n 1 < <(curl %s:%s))"; echo "$x" ; if [ "$x" = "Hello World!" ]; then echo "succeeded"; exit 0; else echo "failed"; exit 1; fi`, FormatIPForURL(host), port)}
	job := NewJob("curl", []string{"/bin/bash", "-c"}, check, JobRetry, JobTTL, JobTimeout)

	return job
}
