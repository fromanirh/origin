package extended

//go:generate go run ./util/annotate -- ./util/annotate/generated/zz_generated.annotations.go

import (
	_ "k8s.io/kubernetes/test/e2e"

	// test sources
	_ "k8s.io/kubernetes/test/e2e/apimachinery"
	_ "k8s.io/kubernetes/test/e2e/apps"
	_ "k8s.io/kubernetes/test/e2e/auth"
	_ "k8s.io/kubernetes/test/e2e/autoscaling"
	_ "k8s.io/kubernetes/test/e2e/common"
	_ "k8s.io/kubernetes/test/e2e/instrumentation"
	_ "k8s.io/kubernetes/test/e2e/kubectl"

	_ "k8s.io/kubernetes/test/e2e/network"
	_ "k8s.io/kubernetes/test/e2e/node"
	_ "k8s.io/kubernetes/test/e2e/scheduling"
	_ "k8s.io/kubernetes/test/e2e/servicecatalog"
	_ "k8s.io/kubernetes/test/e2e/storage"

	_ "github.com/openshift/origin/test/extended/apiserver"
	_ "github.com/openshift/origin/test/extended/authentication"
	_ "github.com/openshift/origin/test/extended/authorization"
	_ "github.com/openshift/origin/test/extended/authorization/rbac"
	_ "github.com/openshift/origin/test/extended/bootstrap_user"
	_ "github.com/openshift/origin/test/extended/builds"
	_ "github.com/openshift/origin/test/extended/cli"
	_ "github.com/openshift/origin/test/extended/cluster"
	_ "github.com/openshift/origin/test/extended/cmd"
	_ "github.com/openshift/origin/test/extended/controller_manager"
	_ "github.com/openshift/origin/test/extended/crdvalidation"
	_ "github.com/openshift/origin/test/extended/csrapprover"
	_ "github.com/openshift/origin/test/extended/deployments"
	_ "github.com/openshift/origin/test/extended/dns"
	_ "github.com/openshift/origin/test/extended/dr"
	_ "github.com/openshift/origin/test/extended/etcd"
	_ "github.com/openshift/origin/test/extended/idling"
	_ "github.com/openshift/origin/test/extended/image_ecosystem"
	_ "github.com/openshift/origin/test/extended/imageapis"
	_ "github.com/openshift/origin/test/extended/images"
	_ "github.com/openshift/origin/test/extended/images/trigger"
	_ "github.com/openshift/origin/test/extended/jobs"
	_ "github.com/openshift/origin/test/extended/localquota"
	_ "github.com/openshift/origin/test/extended/machines"
	_ "github.com/openshift/origin/test/extended/marketplace"
	_ "github.com/openshift/origin/test/extended/networking"
	_ "github.com/openshift/origin/test/extended/oauth"
	_ "github.com/openshift/origin/test/extended/operators"
	_ "github.com/openshift/origin/test/extended/project"
	_ "github.com/openshift/origin/test/extended/prometheus"
	_ "github.com/openshift/origin/test/extended/quota"
	_ "github.com/openshift/origin/test/extended/router"
	_ "github.com/openshift/origin/test/extended/security"
	_ "github.com/openshift/origin/test/extended/templates"
	_ "github.com/openshift/origin/test/extended/topology_manager"
	_ "github.com/openshift/origin/test/extended/user"
)
