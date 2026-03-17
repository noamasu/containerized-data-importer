package common

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	cdiv1 "kubevirt.io/containerized-data-importer-api/pkg/apis/core/v1beta1"
	"kubevirt.io/containerized-data-importer/pkg/common"
	sdkapi "kubevirt.io/controller-lifecycle-operator-sdk/api"
)

var _ = Describe("GetRequestedImageSize", func() {
	It("Should return 1G if 1G provided", func() {
		result, err := GetRequestedImageSize(CreatePvc("testPVC", "default", nil, nil))
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(Equal("1G"))
	})

	It("Should return error and blank if no size provided", func() {
		result, err := GetRequestedImageSize(createPvcNoSize("testPVC", "default", nil, nil))
		Expect(err).To(HaveOccurred())
		Expect(result).To(Equal(""))
	})
})

var _ = Describe("GetStorageClassByName", func() {
	It("Should return the default storage class name", func() {
		client := CreateClient(
			CreateStorageClass("test-storage-class-1", nil),
			CreateStorageClass("test-storage-class-2", map[string]string{
				AnnDefaultStorageClass: "true",
			}),
		)
		sc, _ := GetStorageClassByNameWithK8sFallback(context.Background(), client, nil)
		Expect(sc.Name).To(Equal("test-storage-class-2"))
	})

	It("Should return nil if there's not default storage class", func() {
		client := CreateClient(
			CreateStorageClass("test-storage-class-1", nil),
			CreateStorageClass("test-storage-class-2", nil),
		)
		sc, _ := GetStorageClassByNameWithK8sFallback(context.Background(), client, nil)
		Expect(sc).To(BeNil())
	})

	It("Should return default virt class even if there's not default k8s storage class", func() {
		client := CreateClient(
			CreateStorageClass("test-storage-class-1", nil),
			CreateStorageClass("test-storage-class-2", map[string]string{
				AnnDefaultVirtStorageClass: "true",
			}),
		)
		sc, _ := GetStorageClassByNameWithVirtFallback(context.Background(), client, nil, cdiv1.DataVolumeKubeVirt)
		Expect(sc.Name).To(Equal("test-storage-class-2"))
	})

	DescribeTable("Should return newer default", func(annotation string) {
		olderSc := CreateStorageClass("test-storage-class-new", map[string]string{
			annotation: "true",
		})
		olderSc.SetCreationTimestamp(metav1.NewTime(time.Now().Add(-1 * time.Second)))
		newerSc := CreateStorageClass("test-storage-class-old", map[string]string{
			annotation: "true",
		})
		newerSc.SetCreationTimestamp(metav1.NewTime(time.Now()))
		client := CreateClient(newerSc, olderSc)
		sc, _ := GetStorageClassByNameWithVirtFallback(context.Background(), client, nil, cdiv1.DataVolumeKubeVirt)
		Expect(sc.Name).To(Equal(newerSc.Name))
	},
		Entry("virt storage class", AnnDefaultVirtStorageClass),
		Entry("k8s storage class", AnnDefaultStorageClass),
	)

	DescribeTable("Should fall back to lexicographic order when same timestamp", func(annotation string) {
		firstSc := CreateStorageClass("test-storage-class-1", map[string]string{
			annotation: "true",
		})
		firstSc.SetCreationTimestamp(metav1.NewTime(time.Now()))
		secondSc := CreateStorageClass("test-storage-class-2", map[string]string{
			annotation: "true",
		})
		secondSc.SetCreationTimestamp(metav1.NewTime(time.Now()))
		client := CreateClient(firstSc, secondSc)
		sc, _ := GetStorageClassByNameWithVirtFallback(context.Background(), client, nil, cdiv1.DataVolumeKubeVirt)
		Expect(sc.Name).To(Equal(firstSc.Name))
	},
		Entry("virt storage class", AnnDefaultVirtStorageClass),
		Entry("k8s storage class", AnnDefaultStorageClass),
	)
})

var _ = Describe("Rebind", func() {
	It("Should return error if PV doesn't exist", func() {
		client := CreateClient()
		pvc := &v1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "testPVC",
				Namespace: "namespace",
			},
			Spec: v1.PersistentVolumeClaimSpec{
				VolumeName: "testPV",
			},
		}
		err := Rebind(context.Background(), client, pvc, pvc)
		Expect(err).To(HaveOccurred())
		Expect(errors.IsNotFound(err)).To(BeTrue())
	})

	It("Should return error if bound to unexpected claim", func() {
		pvc := &v1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "testPVC",
				Namespace: "namespace",
			},
			Spec: v1.PersistentVolumeClaimSpec{
				VolumeName: "testPV",
			},
		}
		pv := &v1.PersistentVolume{
			ObjectMeta: metav1.ObjectMeta{
				Name: "testPV",
			},
			Spec: v1.PersistentVolumeSpec{
				ClaimRef: &v1.ObjectReference{
					Name:      "anotherPVC",
					Namespace: "namespace",
					UID:       "uid",
				},
			},
		}
		client := CreateClient(pv)
		err := Rebind(context.Background(), client, pvc, pvc)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal("PV testPV bound to unexpected claim anotherPVC"))
	})
	It("Should return nil if bound to target claim", func() {
		pvc := &v1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "testPVC",
				Namespace: "namespace",
			},
			Spec: v1.PersistentVolumeClaimSpec{
				VolumeName: "testPV",
			},
		}
		targetPVC := pvc.DeepCopy()
		targetPVC.Name = "targetPVC"
		targetPVC.UID = "uid"
		pv := &v1.PersistentVolume{
			ObjectMeta: metav1.ObjectMeta{
				Name: "testPV",
			},
			Spec: v1.PersistentVolumeSpec{
				ClaimRef: &v1.ObjectReference{
					Name:      "targetPVC",
					Namespace: "namespace",
					UID:       "uid",
				},
			},
		}
		client := CreateClient(pv)
		err := Rebind(context.Background(), client, pvc, targetPVC)
		Expect(err).ToNot(HaveOccurred())
	})
	It("Should rebind pv to target claim", func() {
		pvc := &v1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "testPVC",
				Namespace: "namespace",
			},
			Spec: v1.PersistentVolumeClaimSpec{
				VolumeName: "testPV",
			},
		}
		targetPVC := pvc.DeepCopy()
		targetPVC.Name = "targetPVC"
		pvc.UID = "uid"
		pv := &v1.PersistentVolume{
			ObjectMeta: metav1.ObjectMeta{
				Name: "testPV",
			},
			Spec: v1.PersistentVolumeSpec{
				ClaimRef: &v1.ObjectReference{
					Name:      "testPVC",
					Namespace: "namespace",
					UID:       "uid",
				},
			},
		}
		AddAnnotation(pv, "someAnno", "somevalue")
		client := CreateClient(pv)
		err := Rebind(context.Background(), client, pvc, targetPVC)
		Expect(err).ToNot(HaveOccurred())
		updatedPV := &v1.PersistentVolume{}
		key := types.NamespacedName{Name: pv.Name, Namespace: pv.Namespace}
		err = client.Get(context.TODO(), key, updatedPV)
		Expect(err).ToNot(HaveOccurred())
		Expect(updatedPV.Spec.ClaimRef.Name).To(Equal(targetPVC.Name))
		//make sure annotations of pv from before rebind dont get deleted
		Expect(pv.Annotations["someAnno"]).To(Equal("somevalue"))
	})

	Context("GetActiveCDI tests", func() {
		createCDI := func(name string, phase sdkapi.Phase) *cdiv1.CDI {
			return &cdiv1.CDI{
				ObjectMeta: metav1.ObjectMeta{
					Name: name,
				},
				Status: cdiv1.CDIStatus{
					Status: sdkapi.Status{
						Phase: phase,
					},
				},
			}
		}

		It("Should return nil if no CDI", func() {
			client := CreateClient()
			cdi, err := GetActiveCDI(context.Background(), client)
			Expect(err).ToNot(HaveOccurred())
			Expect(cdi).To(BeNil())
		})

		It("Should return single active", func() {
			client := CreateClient(
				createCDI("cdi1", sdkapi.PhaseDeployed),
			)
			cdi, err := GetActiveCDI(context.Background(), client)
			Expect(err).ToNot(HaveOccurred())
			Expect(cdi).ToNot(BeNil())
		})

		It("Should return success with single active one error", func() {
			client := CreateClient(
				createCDI("cdi1", sdkapi.PhaseDeployed),
				createCDI("cdi2", sdkapi.PhaseError),
			)
			cdi, err := GetActiveCDI(context.Background(), client)
			Expect(err).ToNot(HaveOccurred())
			Expect(cdi).ToNot(BeNil())
			Expect(cdi.Name).To(Equal("cdi1"))
		})

		It("Should return error if multiple CDIs are active", func() {
			client := CreateClient(
				createCDI("cdi1", sdkapi.PhaseDeployed),
				createCDI("cdi2", sdkapi.PhaseDeployed),
			)
			cdi, err := GetActiveCDI(context.Background(), client)
			Expect(err).To(HaveOccurred())
			Expect(cdi).To(BeNil())
		})

		It("Should return error if multiple CDIs are error", func() {
			client := CreateClient(
				createCDI("cdi1", sdkapi.PhaseError),
				createCDI("cdi2", sdkapi.PhaseError),
			)
			cdi, err := GetActiveCDI(context.Background(), client)
			Expect(err).To(HaveOccurred())
			Expect(cdi).To(BeNil())
		})

	})
})

var _ = Describe("GetMetricsURL", func() {
	makePod := func(ip string, withMetrics bool) *v1.Pod {
		pod := &v1.Pod{
			Status: v1.PodStatus{
				PodIP: ip,
			},
		}

		if !withMetrics {
			return pod
		}

		pod.Spec = v1.PodSpec{
			Containers: []v1.Container{
				{
					Ports: []v1.ContainerPort{
						{Name: "metrics", ContainerPort: 8080},
					},
				},
			},
		}

		return pod
	}

	It("Should succeed with IPv4", func() {
		pod := makePod("127.0.0.1", true)
		url, err := GetMetricsURL(pod)
		Expect(err).ToNot(HaveOccurred())
		Expect(url).To(Equal("https://127.0.0.1:8080/metrics"))
	})

	It("Should succeed with IPv6", func() {
		pod := makePod("::1", true)
		url, err := GetMetricsURL(pod)
		Expect(err).ToNot(HaveOccurred())
		Expect(url).To(Equal("https://[::1]:8080/metrics"))
	})

	It("Should fail when there is no metrics port", func() {
		pod := makePod("127.0.0.1", false)
		_, err := GetMetricsURL(pod)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("CopyAllowedLabels", func() {
	const (
		testKubevirtIoKey               = "test.kubevirt.io/test"
		testKubevirtIoValue             = "testvalue"
		testInstancetypeKubevirtIoKey   = "instancetype.kubevirt.io/default-preference"
		testInstancetypeKubevirtIoValue = "testpreference"
		testKubevirtIoKeyExisting       = "test.kubevirt.io/existing"
		testKubevirtIoValueExisting     = "existing"
		testKubevirtIoNewValueExisting  = "newvalue"
		testUndesiredKey                = "undesired.key"
		testCdiDatasourceKey            = "cdi.kubevirt.io/storage.import.datasource-name"
		testCdiDatasourceKeyValue       = "testdatasource"
	)

	It("Should copy desired labels", func() {
		srcLabels := map[string]string{
			testKubevirtIoKey:             testKubevirtIoValue,
			testInstancetypeKubevirtIoKey: testInstancetypeKubevirtIoValue,
			testUndesiredKey:              "undesired.key",
			testCdiDatasourceKey:          testCdiDatasourceKeyValue,
		}
		ds := &cdiv1.DataSource{}
		CopyAllowedLabels(srcLabels, ds, false)
		Expect(ds.Labels).To(HaveKeyWithValue(testKubevirtIoKey, testKubevirtIoValue))
		Expect(ds.Labels).To(HaveKeyWithValue(testInstancetypeKubevirtIoKey, testInstancetypeKubevirtIoValue))
		Expect(ds.Labels).To(HaveKeyWithValue(testCdiDatasourceKey, testCdiDatasourceKeyValue))
		Expect(ds.Labels).ToNot(HaveKey(testUndesiredKey))
	})

	DescribeTable("Should overwrite existing labels", func(overwrite bool) {
		srcLabels := map[string]string{
			testKubevirtIoKeyExisting: testKubevirtIoNewValueExisting,
		}
		ds := &cdiv1.DataSource{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					testKubevirtIoKeyExisting: testKubevirtIoValueExisting,
				},
			},
		}
		CopyAllowedLabels(srcLabels, ds, overwrite)
		if overwrite {
			Expect(ds.Labels).To(HaveKeyWithValue(testKubevirtIoKeyExisting, testKubevirtIoNewValueExisting))
		} else {
			Expect(ds.Labels).To(HaveKeyWithValue(testKubevirtIoKeyExisting, testKubevirtIoValueExisting))
		}
	},
		Entry("when override enabled", true),
		Entry("not when override disabled", false),
	)
})

var _ = Describe("sortEvents", func() {
	It("Should sort events by timestamp but prioritize longer messages", func() {
		events := &v1.EventList{
			Items: []v1.Event{
				{LastTimestamp: metav1.NewTime(time.Now().Add(-3 * time.Second)), Message: "third"},
				{LastTimestamp: metav1.NewTime(time.Now().Add(-2 * time.Second)), Message: "second"},
				{LastTimestamp: metav1.NewTime(time.Now().Add(-1 * time.Second)), Message: "first"},
				{LastTimestamp: metav1.NewTime(time.Now().Add(-1 * time.Second)), Message: "first long message"},
			},
		}
		sortEvents(events, false, "")
		Expect(events.Items[0].Message).To(Equal("first long message"))
		Expect(events.Items[1].Message).To(Equal("first"))
		Expect(events.Items[2].Message).To(Equal("second"))
		Expect(events.Items[3].Message).To(Equal("third"))

	})

	It("Should sort events by timestamp but prioritize prime messages", func() {
		events := &v1.EventList{
			Items: []v1.Event{
				{LastTimestamp: metav1.NewTime(time.Now().Add(-4 * time.Second)), Message: "[primeName] second prime"},
				{LastTimestamp: metav1.NewTime(time.Now().Add(-3 * time.Second)), Message: "second"},
				{LastTimestamp: metav1.NewTime(time.Now().Add(-2 * time.Second)), Message: "[primeName] first prime"},
				{LastTimestamp: metav1.NewTime(time.Now().Add(-2 * time.Second)), Message: "[primeName] first prime but more interesting"},
				{LastTimestamp: metav1.NewTime(time.Now().Add(-1 * time.Second)), Message: "first"},
			},
		}
		sortEvents(events, true, "primeName")
		Expect(events.Items[0].Message).To(Equal("[primeName] first prime but more interesting"))
		Expect(events.Items[1].Message).To(Equal("[primeName] first prime"))
		Expect(events.Items[2].Message).To(Equal("[primeName] second prime"))
		Expect(events.Items[3].Message).To(Equal("first"))
		Expect(events.Items[4].Message).To(Equal("second"))
	})
})

var _ = Describe("InflateSizeWithOverhead", func() {
	const (
		storageClassName = "test-sc"
		fsOverhead       = "0.055"
	)

	newCDIConfig := func(globalOverhead cdiv1.Percent, scOverrides map[string]cdiv1.Percent) *cdiv1.CDIConfig {
		return &cdiv1.CDIConfig{
			ObjectMeta: metav1.ObjectMeta{Name: common.ConfigName},
			Status: cdiv1.CDIConfigStatus{
				FilesystemOverhead: &cdiv1.FilesystemOverhead{
					Global:       globalOverhead,
					StorageClass: scOverrides,
				},
			},
		}
	}

	newStorageProfile := func(scName string, annotations map[string]string) *cdiv1.StorageProfile {
		return &cdiv1.StorageProfile{
			ObjectMeta: metav1.ObjectMeta{
				Name:        scName,
				Annotations: annotations,
			},
		}
	}

	fsPvcSpec := func(scName string) *v1.PersistentVolumeClaimSpec {
		fsMode := v1.PersistentVolumeFilesystem
		return &v1.PersistentVolumeClaimSpec{
			StorageClassName: ptr.To(scName),
			VolumeMode:       &fsMode,
		}
	}

	blockPvcSpec := func(scName string) *v1.PersistentVolumeClaimSpec {
		blockMode := v1.PersistentVolumeBlock
		return &v1.PersistentVolumeClaimSpec{
			StorageClassName: ptr.To(scName),
			VolumeMode:       &blockMode,
		}
	}

	It("should apply overhead on top of the minimum when requested size < minimum (filesystem)", func() {
		oneGi := resource.MustParse("1Gi")
		fourGi := resource.MustParse("4Gi")

		cl := CreateClient(
			CreateStorageClass(storageClassName, nil),
			newCDIConfig("0", map[string]cdiv1.Percent{storageClassName: cdiv1.Percent(fsOverhead)}),
			newStorageProfile(storageClassName, map[string]string{
				AnnMinimumSupportedPVCSize: "4Gi",
			}),
		)

		result, err := InflateSizeWithOverhead(context.Background(), cl, oneGi.Value(), fsPvcSpec(storageClassName))
		Expect(err).ToNot(HaveOccurred())

		// The minimum (4Gi) must be enforced first, then overhead applied on top.
		// So the result must be strictly greater than 4Gi.
		Expect(result.Cmp(fourGi)).To(Equal(1),
			"expected result (%s) > minimum (4Gi): overhead must be applied after enforcing minimum", result.String())
	})

	It("should apply overhead on top of the requested size when it already exceeds the minimum (filesystem)", func() {
		fiveGi := resource.MustParse("5Gi")

		cl := CreateClient(
			CreateStorageClass(storageClassName, nil),
			newCDIConfig("0", map[string]cdiv1.Percent{storageClassName: cdiv1.Percent(fsOverhead)}),
			newStorageProfile(storageClassName, map[string]string{
				AnnMinimumSupportedPVCSize: "4Gi",
			}),
		)

		result, err := InflateSizeWithOverhead(context.Background(), cl, fiveGi.Value(), fsPvcSpec(storageClassName))
		Expect(err).ToNot(HaveOccurred())

		Expect(result.Cmp(fiveGi)).To(Equal(1),
			"expected result (%s) > requested (5Gi): overhead must be applied", result.String())
	})

	It("should enforce minimum without overhead for block mode", func() {
		oneGi := resource.MustParse("1Gi")
		fourGi := resource.MustParse("4Gi")

		cl := CreateClient(
			CreateStorageClass(storageClassName, nil),
			newCDIConfig("0", nil),
			newStorageProfile(storageClassName, map[string]string{
				AnnMinimumSupportedPVCSize: "4Gi",
			}),
		)

		result, err := InflateSizeWithOverhead(context.Background(), cl, oneGi.Value(), blockPvcSpec(storageClassName))
		Expect(err).ToNot(HaveOccurred())

		Expect(result.Cmp(fourGi)).To(Equal(0),
			"expected result (%s) == minimum (4Gi) for block mode (no overhead)", result.String())
	})

	It("should return exact requested size for block mode when no minimum is set", func() {
		twoGi := resource.MustParse("2Gi")

		cl := CreateClient(
			CreateStorageClass(storageClassName, nil),
			newCDIConfig("0", nil),
			newStorageProfile(storageClassName, nil),
		)

		result, err := InflateSizeWithOverhead(context.Background(), cl, twoGi.Value(), blockPvcSpec(storageClassName))
		Expect(err).ToNot(HaveOccurred())

		Expect(result.Cmp(twoGi)).To(Equal(0),
			"expected result (%s) == requested (2Gi) for block mode without minimum", result.String())
	})

	It("should apply overhead even when no minimum is configured (filesystem)", func() {
		oneGi := resource.MustParse("1Gi")

		cl := CreateClient(
			CreateStorageClass(storageClassName, nil),
			newCDIConfig("0", map[string]cdiv1.Percent{storageClassName: cdiv1.Percent(fsOverhead)}),
			newStorageProfile(storageClassName, nil),
		)

		result, err := InflateSizeWithOverhead(context.Background(), cl, oneGi.Value(), fsPvcSpec(storageClassName))
		Expect(err).ToNot(HaveOccurred())

		Expect(result.Cmp(oneGi)).To(Equal(1),
			"expected result (%s) > requested (1Gi): overhead must be applied", result.String())
	})
})

func createPvcNoSize(name, ns string, annotations, labels map[string]string) *v1.PersistentVolumeClaim {
	return &v1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   ns,
			Annotations: annotations,
			Labels:      labels,
		},
	}
}
