package utills

import (
	"context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"os"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	PodNamespaceEnv = "POD_NAMESPACE"

	KubeConfigSecretName = "admin-kubeconfig"
	KubeConfigSecretKey  = "kubeconfig"

	//SyncSvcNameAnnotation SyncSvcNamespaceAnnotation are used to back track from the synced Service in the infra
	//cluster to the OG Serivce in the hosted cluster
	SyncSvcNameAnnotation      = "dana.io/source-svc-name"
	SyncSvcNamespaceAnnotation = "dana.io/source-svc-namespace"

	//ClusterNameLabel common label for hosted cluster virt-lunchers
	ClusterNameLabel = "cluster.x-k8s.io/cluster-name"
)

func GetCurrentNs() string {
	return os.Getenv(PodNamespaceEnv)
}

func GetHostedKubeConfig(c client.Client) ([]byte, error) {
	kubeconfig := &corev1.Secret{}
	secretNamespacedName := types.NamespacedName{
		Namespace: GetCurrentNs(),
		Name:      KubeConfigSecretName,
	}
	if err := c.Get(context.Background(), secretNamespacedName, kubeconfig); err != nil {
		return nil, err
	}
	return kubeconfig.Data[KubeConfigSecretKey], nil
}

func GetHostedKubeRestConfig(c client.Client) (*rest.Config, error) {
	config, err := GetHostedKubeConfig(c)
	if err != nil {
		return nil, err
	}
	clientConfig, err := clientcmd.NewClientConfigFromBytes(config)
	if err != nil {
		return nil, err
	}
	return clientConfig.ClientConfig()
}

//GetInfraSvcFromHostedSvc creates a Service object for the infra cluster
//TODO will probably require modification to fit our LB solution
func GetInfraSvcFromHostedSvc(hostedSvc *corev1.Service, clusterGenName string) *corev1.Service {
	infraSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: hostedSvc.Name + "-",
			Namespace:    GetCurrentNs(),
			Annotations:  map[string]string{SyncSvcNameAnnotation: hostedSvc.Name, SyncSvcNamespaceAnnotation: hostedSvc.Namespace},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{ //TODO currently I assume there will be only one port for each service, I dont know if its a use-case but probably should handle this
				Name:        hostedSvc.Spec.Ports[0].Name,
				Protocol:    hostedSvc.Spec.Ports[0].Protocol,
				AppProtocol: hostedSvc.Spec.Ports[0].AppProtocol,
				Port:        hostedSvc.Spec.Ports[0].Port,
				TargetPort:  intstr.IntOrString{IntVal: hostedSvc.Spec.Ports[0].NodePort},
			}},
			Selector: map[string]string{ClusterNameLabel: clusterGenName},
			Type:     "LoadBalancer",
		},
	}
	return infraSvc
}

//GetClusterGenName get the cluster generated name to use as a selector for the synced service
//we get the name from a Cluster CR in the current ns, since we don't know the name of the CR we must use List instead of Get
//we assume there will be only on instance of the Cluster CR in a ns
//TODO find a better way to get cluster gen name
func GetClusterGenName(ctx context.Context, c client.Client) (string, error) {
	cluster := &unstructured.UnstructuredList{}
	cluster.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "cluster.x-k8s.io",
		Version: "v1beta1",
		Kind:    "Cluster",
	})
	if err := c.List(ctx, cluster, client.InNamespace(GetCurrentNs())); err != nil {
		return "", err
	}

	return cluster.Items[0].GetName(), nil
}

func IsObjectMissing(object client.Object) bool {
	if object.GetName() == "" {
		return true
	}
	return false
}
