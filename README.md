# cronjob-label-controller

This repository implements a simple controller for adding a custom label to all cronjobs.

It is is based on
[sample-controller](https://github.com/kubernetes/sample-controller), but has
been stripped down to the bare minimum.  It will ensure that any CronJob (and
the jobs and pods created by that CronJob) have a (by default) `cronjob` label
matching the name of the source CronJob.

The original (sample-controller) code used a workqueue for the purpose of rate
limiting and preventing race conditions.  That code is gone.  CronJob objects
are only updated rarely, which should grately reduces the risk of such issues.

## Running

### Local Development

```sh
# assumes you have a working kubeconfig, not required if operating in-cluster
go build -o cronjob-label-controller .
./cronjob-label-controller -kubeconfig=$HOME/.kube/config
```

### In a cluster

See the [sample kubectl configs](cronjob-label-controller.yaml).

## Why

The default metadata defined on CronJobs, the Jobs they create, and the Pods
they create don't have enough information to connect them together consistently.

[This blog
post](https://medium.com/@tristan_96324/prometheus-k8s-cronjob-alerts-94bee7b90511)
proposes adding a cronjob label to make it easier to associate them.  This
controller implements that.

### Example Rules

```yaml
rules:
- record: cronjob:succeeded_at:sum
  expr: |
    label_replace(
        # most recent success time, as long as the job is still kept around in k8s
        MAX BY(exported_namespace, label_cronjob)(
            kube_job_status_completion_time
                * ON(exported_namespace, job_name) GROUP_RIGHT()
            kube_job_labels{label_cronjob!=""}
        )
        OR ON(exported_namespace, label_cronjob)(
        # add back the failures with value 0, and a little bit of weirdness to limit labels
            cronjob:kube_job_status:sum == 0
                + on (exported_namespace, label_cronjob)
            cronjob:kube_job_status:sum==0
        ),
    "cronjob", "$1", "label_cronjob", "(.+)")
- record: cronjob:kube_job_status:sum
  # the last run succeeded (>0) or failed (=0)
  expr: |
    job_cronjob:kube_job_status_succeeded:sum
      OR ON (exported_namespace, label_cronjob)
    job_cronjob:kube_job_status_failed:sum * 0
- record: job_cronjob:kube_job_status_failed:sum
  # jobs where the most recent run failed
  expr: |
    clamp_max(job_cronjob:kube_job_status_start_time:max, 1)
      * ON(exported_namespace, job_name) GROUP_LEFT()
    kube_job_status_failed > 0
- record: job_cronjob:kube_job_status_succeeded:sum
  # jobs where the most recent run succeeded
  expr: |
    clamp_max(job_cronjob:kube_job_status_start_time:max, 1)
      * ON(exported_namespace, job_name) GROUP_LEFT()
    kube_job_status_succeeded > 0
- record: job_cronjob:kube_job_status_start_time:max
  # find the most recently started run (job) for a cronjob.
  expr: |
    label_replace(
        max(
          kube_job_status_start_time
            * ON(exported_namespace, job_name) GROUP_RIGHT()
          kube_job_labels{label_cronjob!=""}
        ) BY (exported_namespace, label_cronjob, job_name)
        == ON(exported_namespace, label_cronjob) GROUP_LEFT()
        max(
          kube_job_status_start_time
            * ON(exported_namespace, job_name) GROUP_RIGHT()
          kube_job_labels{label_cronjob!=""}
        ) BY (exported_namespace, label_cronjob),
    "cronjob", "$1", "label_cronjob", "(.+)")
- alert: KCronJobStale
  expr: |
    cronjob:succeeded_ago:sum{cronjob="somejob",exported_namespace="ns"} > 86400
  for: 10m
  labels:
    severity: email
```

## Future developments

Consider using the [controller-runtime project](https://github.com/kubernetes-sigs/controller-runtime).
