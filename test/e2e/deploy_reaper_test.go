package e2e

import (
	"context"
	"fmt"
	"time"

	reapergo "github.com/jsanda/reaper-client-go/reaper"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	api "github.com/thelastpickle/reaper-operator/api/v1alpha1"
	"github.com/thelastpickle/reaper-operator/test/framework"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

const (
	defaultRetryInterval = 30 * time.Second
	defaultTimeout       = 3 * time.Minute

	// kustomize currently does not support setting fields dynamically at runtime from the
	// command line. For now the namespace has to match the namespace declared in the
	// test/config/test_dir/kustomization.yaml file. See
	//https://github.com/kubernetes-sigs/kustomize/issues/1113 for more info.
	namespace = "deploy-reaper-test"

	// If you change this, you will also need to update the selector in test/config/deploy_reaper_test/nodeport-service.yaml
	reaperName = "cass-backend"
)

var (
	namespaceBase = "reaper-cass-backend"
)

var _ = Describe("Deploy Reaper with Cassandra backend", func() {
	Context("When a Cassandra cluster is deployed", func() {
		Specify("Reaper is deployed", func() {
			By("create namespace " + namespace)
			err := framework.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())

			By("deploy cass-operator and reaper-operator")
			framework.KustomizeAndApply(namespace, "deploy_reaper_test")

			By("wait for cass-operator to be ready")
			err = framework.WaitForCassOperatorReady(namespace)
			Expect(err).ToNot(HaveOccurred(), "failed waiting for cass-operator to become ready")

			By("wait for reaper-operator to be ready")
			err = framework.WaitForReaperOperatorReady(namespace)
			Expect(err).ToNot(HaveOccurred(), "failed waiting for reaper-operator to become ready")

			By("wait for cassdc to be ready")
			cassdcKey := types.NamespacedName{Namespace: namespace, Name: "reaper-test"}
			cassdcRetryInterval := 15 * time.Second
			cassdcTimeout := 7 * time.Minute
			err = framework.WaitForCassDcReady(cassdcKey, cassdcRetryInterval, cassdcTimeout)
			Expect(err).ToNot(HaveOccurred(), "failed waiting for cassdc to become ready")

			cassdc, err := framework.GetCassDc(cassdcKey)
			Expect(err).ToNot(HaveOccurred(), "failed to get cassdc")

			By("deploy reaper")
			reaper := &api.Reaper{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      reaperName,
				},
				Spec: api.ReaperSpec{
					ServerConfig: api.ServerConfig{
						JmxUserSecretName: "reaper-jmx",
						StorageType: api.StorageTypeCassandra,
						CassandraBackend: &api.CassandraBackend{
							ClusterName:      cassdc.Spec.ClusterName,
							CassandraService: cassdc.GetDatacenterServiceName(),
						},
					},
				},
			}

			err = framework.Client.Create(context.Background(), reaper)
			Expect(err).ToNot(HaveOccurred(), "failed to create reaper object")

			By("wait for reaper to become ready")
			reaperKey := types.NamespacedName{Namespace: reaper.Namespace, Name: reaper.Name}
			err = framework.WaitForReaperReady(reaperKey, 10*time.Second, 3*time.Minute)
			Expect(err).ToNot(HaveOccurred(), "failed waiting for reaper to become ready")

			By("create reaper REST client")
			restClient, err := createRESTClient()
			Expect(err).ToNot(HaveOccurred(), "failed to create REST client")

			By("register cluster with reaper")
			err = restClient.AddCluster(context.Background(), cassdc.Spec.ClusterName, cassdc.GetDatacenterServiceName())
			Expect(err).ToNot(HaveOccurred(), "failed to add cluster with REST api")
		})
	})
})

func createRESTClient() (reapergo.ReaperClient, error) {
	serviceAddr, err := framework.GetNodePortServiceAddress(types.NamespacedName{Namespace: namespace, Name: "reaper-nodeport"}, "app")
	if err != nil {
		return nil, err
	}

	if restClient, err := reapergo.NewReaperClient(fmt.Sprintf("http://%s", serviceAddr)); err == nil {
		return restClient, nil
	} else {
		return nil, fmt.Errorf("failed to create REST client: %w", err)
	}
}
