package topologymanager

import (
	"context"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	exutil "github.com/openshift/origin/test/extended/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
	kubeletconfigv1beta1 "k8s.io/kubelet/config/v1beta1"
	e2e "k8s.io/kubernetes/test/e2e/framework"
	"sigs.k8s.io/yaml"

	g "github.com/onsi/ginkgo"
	o "github.com/onsi/gomega"
)

const (
	strictCheckEnvVar = "TOPOLOGY_MANAGER_TEST_STRICT"

	defaultRoleWorker   = "worker"
	defaultResourceName = "openshift.io/intelnics"

	namespaceMachineConfigOperator = "openshift-machine-config-operator"
	containerMachineConfigDaemon   = "machine-config-daemon"

	featureGateTopologyManager = "TopologyManager"
)

const (
	labelRole     = "node-role.kubernetes.io"
	labelHostname = "kubernetes.io/hostname"
)

const (
	filePathKubeletConfig = "/etc/kubernetes/kubelet.conf"
)

func getRoleWorkerLabel() string {
	roleWorker := defaultRoleWorker
	if rw, ok := os.LookupEnv("ROLE_WORKER"); ok {
		roleWorker = rw
	}
	e2e.Logf("role worker: %q", roleWorker)
	return roleWorker
}

func getDeviceResourceName() string {
	resourceName := defaultResourceName
	if rn, ok := os.LookupEnv("RESOURCE_NAME"); ok {
		resourceName = rn
	}
	e2e.Logf("resource name: %q", resourceName)
	return resourceName
}

func expectNonZeroNodes(nodes []corev1.Node, message string) {
	if _, ok := os.LookupEnv(strictCheckEnvVar); ok {
		o.Expect(nodes).ToNot(o.BeEmpty(), message)
	}
	if len(nodes) < 1 {
		g.Skip(message)
	}
}

func findNodeWithMultiNuma(nodes []corev1.Node, c clientset.Interface, oc *exutil.CLI) (*corev1.Node, int) {
	for _, node := range nodes {
		numaNodes, err := getNumaNodeCountFromNode(c, oc, &node)
		if err != nil {
			e2e.Logf("error getting the NUMA node count from %q: %v", node.Name, err)
			continue
		}
		if numaNodes > 1 {
			return &node, numaNodes
		}
	}
	return nil, 0
}

func filterNodeWithResource(workerNodes []corev1.Node, resourceName string) []corev1.Node {
	var enabledNodes []corev1.Node

	for _, node := range workerNodes {
		e2e.Logf("Node %q status allocatable: %v", node.Name, node.Status.Allocatable)
		if val, ok := node.Status.Allocatable[corev1.ResourceName(resourceName)]; ok {
			v := val.Value()
			if v > 0 {
				enabledNodes = append(enabledNodes, node)
			}
		}
	}
	return enabledNodes
}

func filterNodeWithTopologyManagerPolicy(workerNodes []corev1.Node, client clientset.Interface, oc *exutil.CLI, policy string) []corev1.Node {
	ocRaw := (*oc).WithoutNamespace()

	var topoMgrNodes []corev1.Node

	for _, node := range workerNodes {
		kubeletConfig, err := getKubeletConfig(client, ocRaw, &node)
		e2e.ExpectNoError(err)

		e2e.Logf("kubelet %s CPU Manager policy: %q", node.Name, kubeletConfig.CPUManagerPolicy)

		// verify topology manager feature gate
		if enabled, ok := kubeletConfig.FeatureGates[featureGateTopologyManager]; !ok || !enabled {
			e2e.Logf("kubelet %s Topology Manager FeatureGate not enabled", node.Name)
			continue
		}

		if kubeletConfig.TopologyManagerPolicy != policy {
			e2e.Logf("kubelet %s Topology Manager policy: %q", node.Name, kubeletConfig.TopologyManagerPolicy)
			continue
		}
		topoMgrNodes = append(topoMgrNodes, node)
	}
	return topoMgrNodes
}

func getNodeByRole(c clientset.Interface, role string) ([]corev1.Node, error) {
	selector, err := labels.Parse(fmt.Sprintf("%s/%s=", labelRole, role))
	if err != nil {
		return nil, err
	}

	nodes := &corev1.NodeList{}
	if nodes, err = c.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{LabelSelector: selector.String()}); err != nil {
		return nil, err
	}
	return nodes.Items, nil
}

func getMachineConfigDaemonByNode(c clientset.Interface, node *corev1.Node) (*corev1.Pod, error) {
	listOptions := metav1.ListOptions{
		FieldSelector: fields.SelectorFromSet(fields.Set{"spec.nodeName": node.Name}).String(),
		LabelSelector: labels.SelectorFromSet(labels.Set{"k8s-app": "machine-config-daemon"}).String(),
	}

	mcds, err := c.CoreV1().Pods(namespaceMachineConfigOperator).List(context.Background(), listOptions)
	if err != nil {
		return nil, err
	}

	if len(mcds.Items) < 1 {
		return nil, fmt.Errorf("failed to get machine-config-daemon pod for the node %q", node.Name)
	}
	return &mcds.Items[0], nil
}

const (
	sysFSNumaNodePath = "/sys/devices/system/node"
)

func getContainerRshArgs(pod *corev1.Pod, cnt *corev1.Container) []string {
	return []string{
		"-n", pod.Namespace,
		"-c", cnt.Name,
		pod.Name,
	}
}

func getEnvironmentVariables(oc *exutil.CLI, pod *corev1.Pod, cnt *corev1.Container) (string, error) {
	initialArgs := getContainerRshArgs(pod, cnt)
	command := []string{"env"}
	args := append(initialArgs, command...)
	return oc.AsAdmin().Run("rsh").Args(args...).Output()
}

func getNumaNodeSysfsList(oc *exutil.CLI, pod *corev1.Pod, cnt *corev1.Container) (string, error) {
	initialArgs := getContainerRshArgs(pod, cnt)
	command := []string{"cat", "/sys/devices/system/node/online"}
	args := append(initialArgs, command...)
	return oc.AsAdmin().Run("rsh").Args(args...).Output()
}

func getNumaNodeCountFromContainer(oc *exutil.CLI, pod *corev1.Pod, cnt *corev1.Container) (int, error) {
	out, err := getNumaNodeSysfsList(oc, pod, cnt)
	if err != nil {
		return 0, err
	}
	nodeNum, err := parseSysfsNodeOnline(out)
	if err != nil {
		return 0, err
	}
	e2e.Logf("pod %q cnt %q NUMA nodes %d", pod.Name, cnt.Name, nodeNum)
	return nodeNum, nil
}

func getAllowedCpuListForContainer(oc *exutil.CLI, pod *corev1.Pod, cnt *corev1.Container) (string, error) {
	initialArgs := getContainerRshArgs(pod, cnt)
	command := []string{
		"grep",
		"Cpus_allowed_list",
		"/proc/self/status",
	}
	args := append(initialArgs, command...)
	return oc.AsAdmin().Run("rsh").Args(args...).Output()
}

func makeAllowedCpuListEnv(out string) string {
	pair := strings.SplitN(out, ":", 2)
	return fmt.Sprintf("CPULIST_ALLOWED=%s\n", strings.TrimSpace(pair[1]))
}

// ExecCommandOnMachineConfigDaemon returns the output of the command execution on the machine-config-daemon pod that runs on the specified node
func execCommandOnMachineConfigDaemon(c clientset.Interface, oc *exutil.CLI, node *corev1.Node, command []string) (string, error) {
	mcd, err := getMachineConfigDaemonByNode(c, node)
	if err != nil {
		return "", err
	}

	initialArgs := []string{
		"-n", namespaceMachineConfigOperator,
		"-c", containerMachineConfigDaemon,
		"--request-timeout", "30",
		mcd.Name,
	}
	args := append(initialArgs, command...)
	return oc.AsAdmin().Run("rsh").Args(args...).Output()
}

// GetKubeletConfig returns KubeletConfiguration loaded from the node /etc/kubernetes/kubelet.conf
func getKubeletConfig(c clientset.Interface, oc *exutil.CLI, node *corev1.Node) (*kubeletconfigv1beta1.KubeletConfiguration, error) {
	command := []string{"cat", path.Join("/rootfs", filePathKubeletConfig)}
	kubeletData, err := execCommandOnMachineConfigDaemon(c, oc, node, command)
	if err != nil {
		return nil, err
	}

	e2e.Logf("command output: %s", kubeletData)
	kubeletConfig := &kubeletconfigv1beta1.KubeletConfiguration{}
	if err := yaml.Unmarshal([]byte(kubeletData), kubeletConfig); err != nil {
		return nil, err
	}
	return kubeletConfig, err
}

func parseSysfsNodeOnline(data string) (int, error) {
	/*
	    The file content is expected to be:
	   "0\n" in one-node case
	   "0-K\n" in N-node case where K=N-1
	*/
	info := strings.TrimSpace(data)
	pair := strings.SplitN(info, "-", 2)
	if len(pair) != 2 {
		return 1, nil
	}
	return strconv.Atoi(pair[1])
}

func getNumaNodeCountFromNode(c clientset.Interface, oc *exutil.CLI, node *corev1.Node) (int, error) {
	command := []string{"cat", "/sys/devices/system/node/online"}
	out, err := execCommandOnMachineConfigDaemon(c, oc, node, command)
	if err != nil {
		return 0, err
	}

	e2e.Logf("command output: %s", out)
	nodeNum, err := parseSysfsNodeOnline(out)
	if err != nil {
		return 0, err
	}
	e2e.Logf("node %q NUMA nodes %d", node.Name, nodeNum)
	return nodeNum, nil
}

func makeBusyboxPod(namespace string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    namespace,
			GenerateName: "test-",
			Labels: map[string]string{
				"test": "",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:    "test",
					Image:   "busybox",
					Command: []string{"sleep", "10h"},
				},
			},
		},
	}
}

func createPodsOnNodeSync(client clientset.Interface, namespace string, node *corev1.Node, testPods ...*corev1.Pod) []*corev1.Pod {
	var updatedPods []*corev1.Pod
	for _, testPod := range testPods {
		if node != nil {
			testPod.Spec.NodeSelector = map[string]string{
				labelHostname: node.Name,
			}
		}

		created, err := client.CoreV1().Pods(namespace).Create(context.Background(), testPod, metav1.CreateOptions{})
		e2e.ExpectNoError(err)

		err = waitForPhase(client, created.Namespace, created.Name, corev1.PodRunning, 5*time.Minute)

		updatedPod, err := client.CoreV1().Pods(created.Namespace).Get(context.Background(), created.Name, metav1.GetOptions{})
		e2e.ExpectNoError(err)

		updatedPods = append(updatedPods, updatedPod)
	}
	return updatedPods
}

func waitForPhase(c clientset.Interface, namespace, name string, phase corev1.PodPhase, timeout time.Duration) error {
	return wait.PollImmediate(time.Second, timeout, func() (bool, error) {
		updatedPod, err := c.CoreV1().Pods(namespace).Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}
		if updatedPod.Status.Phase == phase {
			return true, nil
		}
		return false, nil
	})
}
