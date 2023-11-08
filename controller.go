/*
Copyright 2017 The Kubernetes Authors.
Copyright 2020 Google LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	batchinformers "k8s.io/client-go/informers/batch/v1"

	"k8s.io/client-go/kubernetes"
	batchlisters "k8s.io/client-go/listers/batch/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

const controllerAgentName = "cronjob-label-controller"

// Controller is the controller implementation
type Controller struct {
	// kubeclientset is a standard kubernetes clientset
	kubeclientset kubernetes.Interface

	cronjobsLister batchlisters.CronJobLister
	cronjobsSynced cache.InformerSynced

	// the label
	label string
}

// NewController returns a new sample controller
func NewController(kubeclientset kubernetes.Interface, cronjobInformer batchinformers.CronJobInformer, label string) *Controller {

	controller := &Controller{
		kubeclientset:  kubeclientset,
		cronjobsLister: cronjobInformer.Lister(),
		cronjobsSynced: cronjobInformer.Informer().HasSynced,
		label:          label,
	}

	klog.Info("Setting up event handlers")
	// Set up an event handler for when CronJob resources change.
	cronjobInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.handleCronJob,
		UpdateFunc: func(old, new interface{}) {
			newDepl := new.(*batchv1.CronJob)
			oldDepl := old.(*batchv1.CronJob)
			if newDepl.ResourceVersion == oldDepl.ResourceVersion {
				// Periodic resync will send update events for all known CronJobs.
				// Two different versions of the same CronJobs will always have different RVs.
				return
			}
			controller.handleCronJob(new)
		},
	})

	return controller
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) error {
	defer utilruntime.HandleCrash()

	// Wait for the caches to be synced before starting workers
	klog.Info("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.cronjobsSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	<-stopCh
	klog.Info("Shutting down ")

	return nil
}

func (c *Controller) addCronJobLabel(m *map[string]string, v string) bool {
	if *m == nil {
		*m =
			make(map[string]string)
	}
	if _, ok := (*m)[c.label]; ok {
		// cronjob label already exists
		return false
	}
	(*m)[c.label] = v
	return true
}

// handleCronjob resource implementing batchv1.CronJob and update it to
// make sure that it has cronjob labels that match the name of the object.
func (c *Controller) handleCronJob(object interface{}) {
	var cj *batchv1.CronJob
	var ok bool
	if cj, ok = object.(*batchv1.CronJob); !ok {
		klog.V(1).Infof("object can not be cast to a CronJob")
		return
	}

	klog.V(1).Infof("Processing CronJob: %s/%s", cj.GetNamespace(), cj.GetName())

	// There are three levels of labels:
	// - on the CronJob object
	// - on the CronJob.Spec.JobTemplate
	// - on the CronJob.Spec.JobTemplate.Spec.Template
	//
	// Explicitly add the label to all three, to not assume any implicit labels.
	ls := []*map[string]string{
		&cj.Labels,
		&cj.Spec.JobTemplate.Labels,
		&cj.Spec.JobTemplate.Spec.Template.Labels,
	}
	updated := false

	for _, l := range ls {
		if c.addCronJobLabel(l, cj.GetName()) {
			updated = true
		}
	}

	// Only update the object on the server if we've changed a label.
	if updated {
		klog.Infof("adding cronjob label to %s/%s", cj.GetNamespace(), cj.GetName())
		_, err := c.kubeclientset.BatchV1().CronJobs(cj.GetNamespace()).Update(context.TODO(), cj, metav1.UpdateOptions{})
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("error updating CronJob %s/%s: %v", cj.GetNamespace(), cj.GetName(), err))
			return
		}
	}
	return
}
