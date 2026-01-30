/*
Copyright 2026.

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

package controller

import (
	"context"
	"reflect"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	cronjobv1 "github.com/senglezou/cronjob-ctrl/api/v1"
)

var _ = Describe("CronJob controller", func() {
	Context("CronJob controller test", func() {

		const CronjobName = "test-cronjob"

		ctx := context.Background()

		namespace := &v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      CronjobName,
				Namespace: CronjobName,
			},
		}

		typeNamespacedName := types.NamespacedName{
			Name:      CronjobName,
			Namespace: CronjobName,
		}
		cronJob := &cronjobv1.CronJob{}

		SetDefaultEventuallyTimeout(2 * time.Minute)
		SetDefaultEventuallyPollingInterval(time.Second)

		BeforeEach(func() {
			By("Creating the Namespace to perform the tests")
			err := k8sClient.Get(ctx, types.NamespacedName{Name: CronjobName}, &v1.Namespace{})
			if err != nil && errors.IsNotFound(err) {
				err = k8sClient.Create(ctx, namespace)
				Expect(err).NotTo(HaveOccurred())
			}

			By("creating the custom resource for the Kind CronJob")
			cronJob = &cronjobv1.CronJob{}
			err = k8sClient.Get(ctx, typeNamespacedName, cronJob)
			if err != nil && errors.IsNotFound(err) {

				cronJob = &cronjobv1.CronJob{
					ObjectMeta: metav1.ObjectMeta{
						Name:      CronjobName,
						Namespace: namespace.Name,
					},
					Spec: cronjobv1.CronJobSpec{
						Schedule: "1 * * * *",
						JobTemplate: batchv1.JobTemplateSpec{
							Spec: batchv1.JobSpec{
								Template: v1.PodTemplateSpec{
									Spec: v1.PodSpec{
										Containers: []v1.Container{
											{
												Name:  "test-container",
												Image: "test-image",
											},
										},
										RestartPolicy: v1.RestartPolicyOnFailure,
									},
								},
							},
						},
					},
				}

				err = k8sClient.Create(ctx, cronJob)
				Expect(err).NotTo(HaveOccurred())
			}
		})

		AfterEach(func() {
			By("removing the custom resource for the Kind CronJob")
			found := &cronjobv1.CronJob{}
			err := k8sClient.Get(ctx, typeNamespacedName, found)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Delete(context.TODO(), found)).To(Succeed())
			}).Should(Succeed())

			// TODO(user): Attention if you improve this code by adding other context test you MUST
			// be aware of the current delete namespace limitations.
			// More info: https://book.kubebuilder.io/reference/envtest.html#testing-considerations
			By("Deleting the Namespace to perform the tests")
			_ = k8sClient.Delete(ctx, namespace)
		})

		It("should successfully reconcile a custom resource for CronJob", func() {
			By("Checking if the custom resource was successfully created")
			Eventually(func(g Gomega) {
				found := &cronjobv1.CronJob{}
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, found)).To(Succeed())
			}).Should(Succeed())

			By("Checking that status conditions are initialized")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, cronJob)).To(Succeed())
				g.Expect(cronJob.Status.Conditions).NotTo(BeEmpty())
			}).Should(Succeed())

			By("Checking that the CronJob has zero active Jobs")
			Consistently(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, cronJob)).To(Succeed())
				g.Expect(cronJob.Status.Active).To(BeEmpty())
			}).WithTimeout(time.Second * 10).WithPolling(time.Millisecond * 250).Should(Succeed())

			By("Creating a new Job owned by the CronJob")
			testJob := &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-job",
					Namespace: namespace.Name,
				},
				Spec: batchv1.JobSpec{
					Template: v1.PodTemplateSpec{
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Name:  "test-container",
									Image: "test-image",
								},
							},
							RestartPolicy: v1.RestartPolicyOnFailure,
						},
					},
				},
			}

			// Note that your CronJob’s GroupVersionKind is required to set up this owner reference.
			kind := reflect.TypeFor[cronjobv1.CronJob]().Name()
			gvk := cronjobv1.GroupVersion.WithKind(kind)

			controllerRef := metav1.NewControllerRef(cronJob, gvk)
			testJob.SetOwnerReferences([]metav1.OwnerReference{*controllerRef})
			Expect(k8sClient.Create(ctx, testJob)).To(Succeed())
			// Note that you can not manage the status values while creating the resource.
			// The status field is managed separately to reflect the current state of the resource.
			// Therefore, it should be updated using a PATCH or PUT operation after the resource has been created.
			// Additionally, it is recommended to use StatusConditions to manage the status. For further information see:
			// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
			testJob.Status.Active = 2
			Expect(k8sClient.Status().Update(ctx, testJob)).To(Succeed())

			By("Checking that the CronJob has one active Job in status")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, cronJob)).To(Succeed())
				g.Expect(cronJob.Status.Active).To(HaveLen(1), "should have exactly one active job")
				g.Expect(cronJob.Status.Active[0].Name).To(Equal("test-job"), "the active job name should match")
			}).Should(Succeed())

			By("Checking the latest Status Condition added to the CronJob instance")
			Expect(k8sClient.Get(ctx, typeNamespacedName, cronJob)).To(Succeed())
			var conditions []metav1.Condition
			Expect(cronJob.Status.Conditions).To(ContainElement(
				HaveField("Type", Equal("Available")), &conditions))
			Expect(conditions).To(HaveLen(1), "should have one Available condition")
			Expect(conditions[0].Status).To(Equal(metav1.ConditionTrue), "Available should be True")
			Expect(conditions[0].Reason).To(Equal("JobsActive"), "reason should be JobsActive")
		})
	})
})
