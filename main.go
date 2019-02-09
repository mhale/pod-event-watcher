// pod-event-watcher is an example program for demonstrating one way to monitor pods.
// It creates a Kubernetes controller that maintains a cache (Store) of pod information and calls event handler functions (AddFunc etc.) when the cache is updated.
// This has a side effect where on initial startup the AddFunc handler will be called once for each pod that is currently running (because the currently running pods are being added to the cache).

package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/go-test/deep"
	"github.com/k0kubun/pp"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

var details *bool

// podCreated is called when a pod is created.
// Pods do not have all of their fields populated at creation time; the information is added with multiple updates after pod creation.
func podCreated(obj interface{}) {
	pod := obj.(*v1.Pod)
	log.Println("Pod created: " + pod.ObjectMeta.Name)
	if *details {
		pp.Println(pod)
	}
}

// podDeleted is called when a pod is deleted.
// Before a pod is deleted, it will be updated with a termination time.
func podDeleted(obj interface{}) {
	pod := obj.(*v1.Pod)
	log.Println("Pod deleted: " + pod.ObjectMeta.Name)
	if *details {
		pp.Print(pod)
	}
}

// podUpdated is called when a pod is updated.
// Pods are updated multiple times immediately after being created, so expect multiple calls for the same pod.
func podUpdated(oldObj, newObj interface{}) {
	oldPod := oldObj.(*v1.Pod)
	newPod := newObj.(*v1.Pod)
	log.Println("Pod updated: " + oldPod.ObjectMeta.Name)
	if *details {
		if diff := deep.Equal(oldPod, newPod); diff != nil {
			log.Printf("Difference: %s\n", pp.Sprint(diff))
		} else {
			log.Println("No difference, just a cache update")
		}
	}
}

// watchPods creates a controller that calls handler functions in response to pod events.
func watchPods(client cache.Getter, namespace string, selector string) cache.Store {
	// Apply the specified selector as a filter.
	optionsModifier := func(options *metav1.ListOptions) {
		options.LabelSelector = selector
	}

	// Create the controller.
	// Note: The AddFunc handler will be called for each existing pod when first starting the controller.
	// Note: The UpdateFunc handler will be called every resync period, even if nothing has changed.
	lw := cache.NewFilteredListWatchFromClient(client, v1.ResourcePods.String(), namespace, optionsModifier)
	resyncPeriod := 5 * time.Minute
	store, controller := cache.NewInformer(lw, &v1.Pod{}, resyncPeriod, cache.ResourceEventHandlerFuncs{
		AddFunc:    podCreated,
		DeleteFunc: podDeleted,
		UpdateFunc: podUpdated,
	})

	// Make the controller run forever (nothing sends to the channel).
	forever := make(chan struct{})
	go controller.Run(forever)

	return store
}

// homeDir gets the user's home directory.
func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return os.Getenv("USERPROFILE") // Windows
}

func main() {
	// Optional path to .kube/config for authentication details.
	// Logging in first may be required to authenticate (and thereby populate .kube/config) if running outside a cluster.
	var kubeconfig *string
	if home := homeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}

	// Optional namespace to watch.
	namespace := flag.String("namespace", metav1.NamespaceAll, "namespace to watch")

	// Optional details display.
	details = flag.Bool("details", false, "print pod object details")

	// Example label selector, which results in the selector string "foo=bar,baz=quux"
	labelSelector := labels.Set(map[string]string{"foo": "bar", "baz": "quux"}).AsSelector()
	selector := flag.String("selector", "", "selector (label query) to filter on (e.g. \""+labelSelector.String()+"\")")

	flag.Parse()

	// Try to use the in-cluster config first, which will succeed if running in a cluster.
	// If that fails, try to use the local .kube/config, which will succeed if running on a user's machine and they have logged in recently.
	config, err := rest.InClusterConfig()
	if err != nil {
		config, err = clientcmd.BuildConfigFromFlags("", *kubeconfig)
		if err != nil {
			panic(err.Error())
		}
	}

	// Create a set of clients for each API group.
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	// Use the core API client.
	client := clientset.Core().RESTClient()

	// Watch for pod events.
	watchPods(client, *namespace, *selector)

	// Wait forever, or until SIGINT is received (ctrl-c).
	select {}
}
