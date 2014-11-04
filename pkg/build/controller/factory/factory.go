package factory

import (
	"errors"
	"time"

	kapi "github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	kclient "github.com/GoogleCloudPlatform/kubernetes/pkg/client"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/client/cache"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/labels"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/runtime"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/watch"

	buildapi "github.com/openshift/origin/pkg/build/api"
	controller "github.com/openshift/origin/pkg/build/controller"
	strategy "github.com/openshift/origin/pkg/build/controller/strategy"
	osclient "github.com/openshift/origin/pkg/client"
)

type BuildControllerFactory struct {
	Client              *osclient.Client
	KubeClient          *kclient.Client
	DockerBuildStrategy *strategy.DockerBuildStrategy
	STIBuildStrategy    *strategy.STIBuildStrategy
}

func (factory *BuildControllerFactory) Create() *controller.BuildController {
	buildStore := cache.NewStore()
	cache.NewReflector(&buildLW{client: factory.Client}, &buildapi.Build{}, buildStore).Run()

	buildQueue := cache.NewFIFO()
	cache.NewReflector(&buildLW{client: factory.Client}, &buildapi.Build{}, buildQueue).Run()

	podQueue := cache.NewFIFO()
	cache.NewPoller(factory.pollPods, 10*time.Second, podQueue).Run()

	return &controller.BuildController{
		BuildStore:   buildStore,
		BuildUpdater: factory.Client,
		PodCreator:   factory.KubeClient,
		NextBuild: func() *buildapi.Build {
			return buildQueue.Pop().(*buildapi.Build)
		},
		NextPod: func() *kapi.Pod {
			return podQueue.Pop().(*kapi.Pod)
		},
		BuildStrategy: &typeBasedFactoryStrategy{
			DockerBuildStrategy: factory.DockerBuildStrategy,
			STIBuildStrategy:    factory.STIBuildStrategy,
		},
	}
}

// pollPods lists all pods and returns an enumerator for cache.Poller.
func (factory *BuildControllerFactory) pollPods() (cache.Enumerator, error) {
	list, err := factory.KubeClient.ListPods(kapi.NewContext(), labels.Everything())
	if err != nil {
		return nil, err
	}
	return &podEnumerator{list}, nil
}

// minionEnumerator allows a cache.Poller to enumerate items in an api.PodList
type podEnumerator struct {
	*kapi.PodList
}

// Len returns the number of items in the pod list.
func (pe *podEnumerator) Len() int {
	if pe.PodList == nil {
		return 0
	}
	return len(pe.Items)
}

// Get returns the item (and ID) with the particular index.
func (pe *podEnumerator) Get(index int) (string, interface{}) {
	return pe.Items[index].ID, &pe.Items[index]
}

type typeBasedFactoryStrategy struct {
	DockerBuildStrategy *strategy.DockerBuildStrategy
	STIBuildStrategy    *strategy.STIBuildStrategy
}

func (f *typeBasedFactoryStrategy) CreateBuildPod(build *buildapi.Build) (*kapi.Pod, error) {
	switch build.Parameters.Strategy.Type {
	case buildapi.DockerBuildStrategyType:
		return f.DockerBuildStrategy.CreateBuildPod(build)
	case buildapi.STIBuildStrategyType:
		return f.STIBuildStrategy.CreateBuildPod(build)
	default:
		return nil, errors.New("No strategy defined for type")
	}
}

// buildLW is a ListWatcher implementation for Builds.
type buildLW struct {
	client osclient.Interface
}

// List lists all Builds.
func (lw *buildLW) List() (runtime.Object, error) {
	return lw.client.ListBuilds(kapi.NewContext(), labels.Everything())
}

// Watch watches all Builds.
func (lw *buildLW) Watch(resourceVersion string) (watch.Interface, error) {
	return lw.client.WatchBuilds(kapi.NewContext(), labels.Everything(), labels.Everything(), "0")
}
