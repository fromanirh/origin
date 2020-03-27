package topologymanager

import (
	"fmt"

	exutil "github.com/openshift/origin/test/extended/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	kubeletconfigv1beta1 "k8s.io/kubelet/config/v1beta1"
	e2e "k8s.io/kubernetes/test/e2e/framework"

	g "github.com/onsi/ginkgo"
	t "github.com/onsi/ginkgo/extensions/table"
	o "github.com/onsi/gomega"
)

var _ = g.Describe("[Serial][sig-node][Feature:TopologyManager] Configured cluster", func() {
	defer g.GinkgoRecover()

	var (
		oc                 = exutil.NewCLI("topology-manager", exutil.KubeConfigPath())
		client             clientset.Interface // shortcut
		roleWorkerLabel    string
		workerNodes        []corev1.Node
		sriovNodes         []corev1.Node
		topoMgrNodes       []corev1.Node
		deviceResourceName string
		err                error
	)

	g.BeforeEach(func() {
		client = oc.KubeFramework().ClientSet
		o.Expect(client).ToNot(o.BeNil())

		roleWorkerLabel = getRoleWorkerLabel()

		workerNodes, err = getNodeByRole(client, roleWorkerLabel)
		e2e.ExpectNoError(err)
		o.Expect(workerNodes).ToNot(o.BeEmpty())

		topoMgrNodes = filterNodeWithTopologyManagerPolicy(workerNodes, client, oc, kubeletconfigv1beta1.SingleNumaNodeTopologyManager)
		expectNonZeroNodes(topoMgrNodes, "topology manager not configured on all nodes")

		deviceResourceName = getDeviceResourceName()
		// deviceResourceName MAY be == "". This means "ignore devices"
		if deviceResourceName != "" {
			sriovNodes = filterNodeWithResource(topoMgrNodes, deviceResourceName)
			// we don't handle yet an uneven device amount on worker nodes. IOW, we expect the same amount of devices on each node
		} else {
			sriovNodes = topoMgrNodes
		}
	})

	g.Context("with non-gu workload", func() {
		t.DescribeTable("should run with no regressions",
			func(pps PodParamsList) {
				ns := oc.KubeFramework().Namespace.Name
				testPods := pps.MakeBusyboxPods(ns, deviceResourceName)
				// we just want to run pods and check they actually go running
				createPodsOnNodeSync(client, ns, nil, testPods...)
				// rely on cascade deletion when namespace is deleted
			},
			// fhbz#1813397 - k8s issue83775
			t.Entry("with single pod, single container requesting 1 core", []PodParams{
				{
					Containers: []ContainerParams{{
						CpuRequest: 1000,
					}},
				},
			}),
			// fhbz#1813397 - k8s issue83775
			t.Entry("with single pod, single container requesting multiple cores", []PodParams{
				{
					Containers: []ContainerParams{{
						CpuRequest: 2500,
					}},
				},
			}),
		)
	})

	g.Context("with gu workload", func() {
		t.DescribeTable("should guarantee NUMA-aligned cpu cores in gu pods",
			func(pps PodParamsList) {
				expectNonZeroNodes(sriovNodes, fmt.Sprintf("device %q not available on all nodes", deviceResourceName))

				if requestCpu, ok := enoughCoresInTheCluster(sriovNodes, pps); !ok {
					g.Skip(fmt.Sprintf("not enough CPU resources in the cluster requested=%v", requestCpu))
				}
				if requestDevices, ok := enoughDevicesInTheCluster(sriovNodes, deviceResourceName, pps); !ok {
					g.Skip(fmt.Sprintf("not enough CPU resources in the cluster requested=%v", requestDevices))
				}

				ns := oc.KubeFramework().Namespace.Name
				testPods := pps.MakeBusyboxPods(ns, deviceResourceName)
				updatedPods := createPodsOnNodeSync(client, ns, nil, testPods...)
				// rely on cascade deletion when namespace is deleted

				for _, pod := range updatedPods {
					for _, cnt := range pod.Spec.Containers {
						out, err := getAllowedCpuListForContainer(oc, pod, &cnt)
						e2e.ExpectNoError(err)
						envOut := makeAllowedCpuListEnv(out)

						podEnv, err := getEnvironmentVariables(oc, pod, &cnt)
						e2e.ExpectNoError(err)
						envOut += podEnv

						e2e.Logf("pod %q container %q: %s", pod.Name, cnt.Name, envOut)

						numaNodes, err := getNumaNodeCount(oc, pod, &cnt)
						e2e.ExpectNoError(err)
						envInfo := testEnvInfo{
							numaNodes:         numaNodes,
							sriovResourceName: deviceResourceName,
						}
						numaRes, err := checkNUMAAlignment(oc.KubeFramework(), pod, &cnt, envOut, &envInfo)
						e2e.ExpectNoError(err)
						ok := numaRes.CheckAlignment()

						o.Expect(ok).To(o.BeTrue(), "misaligned NUMA resources: %s", numaRes.String())
					}

				}

			},
			t.Entry("with single pod, single container requesting 1 core, 1 device", []PodParams{
				{
					Containers: []ContainerParams{{
						CpuRequest:    1000,
						CpuLimit:      1000,
						DeviceRequest: 1,
						DeviceLimit:   1,
					}},
				},
			}),
			t.Entry("with single pod, single container requesting 4 cores, 1 device", []PodParams{
				{
					// 4 cores is a random value. Anything >= 2, to make sure to request 2 physical cores also if HT is enabled, is fine
					Containers: []ContainerParams{{
						CpuRequest:    4000,
						CpuLimit:      4000,
						DeviceRequest: 1,
						DeviceLimit:   1,
					}},
				},
			}),
			t.Entry("with single pod, multiple containers requesting 1 core, 1 device each", []PodParams{
				{
					Containers: []ContainerParams{{
						CpuRequest:    1000,
						CpuLimit:      1000,
						DeviceRequest: 1,
						DeviceLimit:   1,
					}, {
						CpuRequest:    1000,
						CpuLimit:      1000,
						DeviceRequest: 1,
						DeviceLimit:   1,
					}},
				},
			}),
			t.Entry("with multiple pods, each with a single container requesting 1 core, 1 device", []PodParams{
				{
					Containers: []ContainerParams{{
						CpuRequest:    1000,
						CpuLimit:      1000,
						DeviceRequest: 1,
						DeviceLimit:   1,
					}},
				},
				{
					Containers: []ContainerParams{{
						CpuRequest:    1000,
						CpuLimit:      1000,
						DeviceRequest: 1,
						DeviceLimit:   1,
					}},
				},
			}),
			t.Entry("with multiple pods, each with a single container requesting 2 core, 1 device", []PodParams{
				{
					Containers: []ContainerParams{{
						CpuRequest:    2000,
						CpuLimit:      2000,
						DeviceRequest: 1,
						DeviceLimit:   1,
					}},
				},
				{
					Containers: []ContainerParams{{
						CpuRequest:    2000,
						CpuLimit:      2000,
						DeviceRequest: 1,
						DeviceLimit:   1,
					}},
				},
			}),
			t.Entry("with multiple pods, each with multiple containers requesting 1 core, 1 device", []PodParams{
				{
					Containers: []ContainerParams{{
						CpuRequest:    1000,
						CpuLimit:      1000,
						DeviceRequest: 1,
						DeviceLimit:   1,
					}, {
						CpuRequest:    1000,
						CpuLimit:      1000,
						DeviceRequest: 1,
						DeviceLimit:   1,
					}},
				},
				{
					Containers: []ContainerParams{{
						CpuRequest:    1000,
						CpuLimit:      1000,
						DeviceRequest: 1,
						DeviceLimit:   1,
					}, {
						CpuRequest:    1000,
						CpuLimit:      1000,
						DeviceRequest: 1,
						DeviceLimit:   1,
					}},
				},
			}),
			t.Entry("with multiple pods, each with multiple containers requesting 1 core, only one requesting 1 device", []PodParams{
				{
					Containers: []ContainerParams{{
						CpuRequest:    2000,
						CpuLimit:      2000,
						DeviceRequest: 1,
						DeviceLimit:   1,
					}, {
						CpuRequest: 2000,
						CpuLimit:   2000,
					}},
				},
				{
					Containers: []ContainerParams{{
						CpuRequest:    2000,
						CpuLimit:      2000,
						DeviceRequest: 1,
						DeviceLimit:   1,
					}, {
						CpuRequest: 2000,
						CpuLimit:   2000,
					}},
				},
			}),
		)
	})
})

// TODO: add test to saturate a NUMA node
// TODO: add negative tests
// TODO: add connectivity tests

type ContainerParams struct {
	CpuRequest    int64 // millicores
	CpuLimit      int64 // millicores
	DeviceRequest int64
	DeviceLimit   int64
}

type PodParams struct {
	Containers []ContainerParams
}

func (pp PodParams) TotalCpuRequest() int64 {
	var total int64
	for _, cnt := range pp.Containers {
		total += cnt.CpuRequest
	}
	return total
}

func (pp PodParams) TotalDeviceRequest() int64 {
	var total int64
	for _, cnt := range pp.Containers {
		total += cnt.DeviceRequest
	}
	return total
}

func (pp PodParams) MakeBusyboxPod(namespace, deviceName string) *corev1.Pod {
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    namespace,
			GenerateName: "test-",
			Labels: map[string]string{
				"test": "",
			},
		},
	}

	for i, cp := range pp.Containers {
		cnt := corev1.Container{
			Name:    fmt.Sprintf("test-%d", i),
			Image:   "busybox",
			Command: []string{"sleep", "10h"},
			Resources: corev1.ResourceRequirements{
				Requests: map[corev1.ResourceName]resource.Quantity{
					corev1.ResourceMemory: resource.MustParse("64Mi"),
				},
				Limits: map[corev1.ResourceName]resource.Quantity{
					corev1.ResourceMemory: resource.MustParse("64Mi"),
				},
			},
		}

		if cp.CpuRequest > 0 {
			cnt.Resources.Requests[corev1.ResourceCPU] = resource.MustParse(fmt.Sprintf("%dm", cp.CpuRequest))
		}
		if cp.CpuLimit > 0 {
			cnt.Resources.Limits[corev1.ResourceCPU] = resource.MustParse(fmt.Sprintf("%dm", cp.CpuLimit))
		}

		if deviceName != "" {
			if cp.DeviceRequest > 0 {
				cnt.Resources.Requests[corev1.ResourceName(deviceName)] = resource.MustParse(fmt.Sprintf("%d", cp.DeviceRequest))
			}
			if cp.DeviceLimit > 0 {
				cnt.Resources.Limits[corev1.ResourceName(deviceName)] = resource.MustParse(fmt.Sprintf("%d", cp.DeviceLimit))
			}
		}
		pod.Spec.Containers = append(pod.Spec.Containers, cnt)
	}

	return &pod
}

type PodParamsList []PodParams

func (pps PodParamsList) MakeBusyboxPods(namespace, deviceName string) []*corev1.Pod {
	var pods []*corev1.Pod
	for _, pp := range pps {
		pods = append(pods, pp.MakeBusyboxPod(namespace, deviceName))
	}
	return pods
}

func (pps PodParamsList) TotalCpuRequest() int64 {
	var total int64
	for _, pp := range pps {
		total += pp.TotalCpuRequest()
	}
	return total
}

func (pps PodParamsList) TotalDeviceRequest() int64 {
	var total int64
	for _, pp := range pps {
		total += pp.TotalDeviceRequest()
	}
	return total
}

func enoughCoresInTheCluster(nodes []corev1.Node, pps PodParamsList) (resource.Quantity, bool) {
	requestCpu := resource.MustParse(fmt.Sprintf("%dm", pps.TotalCpuRequest()))
	e2e.Logf("checking request %v on %d nodes", requestCpu, len(nodes))

	for _, node := range nodes {
		availCpu, ok := node.Status.Allocatable[corev1.ResourceCPU]
		o.Expect(ok).To(o.BeTrue())
		o.Expect(availCpu.IsZero()).To(o.BeFalse())

		e2e.Logf("node %q available cpu %v requested cpu %v", node.Name, availCpu, requestCpu)
		if availCpu.Cmp(requestCpu) >= 1 {
			e2e.Logf("at least node %q has enough resources, cluster OK", node.Name)
			return requestCpu, true
		}
	}

	return requestCpu, false
}

func enoughDevicesInTheCluster(nodes []corev1.Node, deviceResourceName string, pps PodParamsList) (resource.Quantity, bool) {
	requestDevs := resource.MustParse(fmt.Sprintf("%d", pps.TotalDeviceRequest()))
	e2e.Logf("checking request %v on %d nodes", requestDevs, len(nodes))

	for _, node := range nodes {
		availDevs, ok := node.Status.Allocatable[corev1.ResourceName(deviceResourceName)]
		o.Expect(ok).To(o.BeTrue())
		o.Expect(availDevs.IsZero()).To(o.BeFalse())

		e2e.Logf("node %q available devs %v requested devs %v", node.Name, availDevs, requestDevs)
		if availDevs.Cmp(requestDevs) >= 1 {
			e2e.Logf("at least node %q has enough resources, cluster OK", node.Name)
			return requestDevs, true
		}
	}

	return requestDevs, false
}
