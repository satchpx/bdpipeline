/*
Copyright 2018 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package webhook

import (
	"fmt"

	"github.com/golang/glog"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/GoogleCloudPlatform/spark-on-k8s-operator/pkg/apis/sparkoperator.k8s.io/v1beta1"
	"github.com/GoogleCloudPlatform/spark-on-k8s-operator/pkg/config"
	"github.com/GoogleCloudPlatform/spark-on-k8s-operator/pkg/util"
)

const (
	sparkDriverContainerName   = "spark-kubernetes-driver"
	sparkExecutorContainerName = "executor"
	maxNameLength              = 63
)

// patchOperation represents a RFC6902 JSON patch operation.
type patchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

func patchSparkPod(pod *corev1.Pod, app *v1beta1.SparkApplication) []patchOperation {
	var patchOps []patchOperation

	if util.IsDriverPod(pod) {
		patchOps = append(patchOps, addOwnerReference(pod, app))
	}
	patchOps = append(patchOps, addVolumes(pod, app)...)
	patchOps = append(patchOps, addGeneralConfigMaps(pod, app)...)
	patchOps = append(patchOps, addSparkConfigMap(pod, app)...)
	patchOps = append(patchOps, addHadoopConfigMap(pod, app)...)
	patchOps = append(patchOps, addTolerations(pod, app)...)
	if pod.Spec.Affinity == nil {
		op := addAffinity(pod, app)
		if op != nil {
			patchOps = append(patchOps, *op)
		}
	}
	if pod.Spec.SecurityContext == nil {
		op := addSecurityContext(pod, app)
		if op != nil {
			patchOps = append(patchOps, *op)
		}
	}

	return patchOps
}

func addOwnerReference(pod *corev1.Pod, app *v1beta1.SparkApplication) patchOperation {
	ownerReference := util.GetOwnerReference(app)

	path := "/metadata/ownerReferences"
	var value interface{}
	if len(pod.OwnerReferences) == 0 {
		value = []metav1.OwnerReference{ownerReference}
	} else {
		path += "/-"
		value = ownerReference
	}

	return patchOperation{Op: "add", Path: path, Value: value}
}

func addVolumes(pod *corev1.Pod, app *v1beta1.SparkApplication) []patchOperation {
	volumes := app.Spec.Volumes
	volumeMap := make(map[string]corev1.Volume)
	for _, v := range volumes {
		volumeMap[v.Name] = v
	}

	var volumeMounts []corev1.VolumeMount
	if util.IsDriverPod(pod) {
		volumeMounts = app.Spec.Driver.VolumeMounts
	} else if util.IsExecutorPod(pod) {
		volumeMounts = app.Spec.Executor.VolumeMounts
	}

	var ops []patchOperation
	for _, m := range volumeMounts {
		if v, ok := volumeMap[m.Name]; ok {
			ops = append(ops, addVolume(pod, v))
			ops = append(ops, addVolumeMount(pod, m))
		}
	}

	return ops
}

func addVolume(pod *corev1.Pod, volume corev1.Volume) patchOperation {
	path := "/spec/volumes"
	var value interface{}
	if len(pod.Spec.Volumes) == 0 {
		value = []corev1.Volume{volume}
	} else {
		path += "/-"
		value = volume
	}

	return patchOperation{Op: "add", Path: path, Value: value}
}

func addVolumeMount(pod *corev1.Pod, mount corev1.VolumeMount) patchOperation {
	i := 0
	// Find the driver or executor container in the pod.
	for ; i < len(pod.Spec.Containers); i++ {
		if pod.Spec.Containers[i].Name == sparkDriverContainerName ||
			pod.Spec.Containers[i].Name == sparkExecutorContainerName {
			break
		}
	}

	path := fmt.Sprintf("/spec/containers/%d/volumeMounts", i)
	var value interface{}
	if len(pod.Spec.Containers[i].VolumeMounts) == 0 {
		value = []corev1.VolumeMount{mount}
	} else {
		path += "/-"
		value = mount
	}

	return patchOperation{Op: "add", Path: path, Value: value}
}

func addEnvironmentVariable(pod *corev1.Pod, envName, envValue string) patchOperation {
	i := 0
	// Find the driver or executor container in the pod.
	for ; i < len(pod.Spec.Containers); i++ {
		if pod.Spec.Containers[i].Name == sparkDriverContainerName ||
			pod.Spec.Containers[i].Name == sparkExecutorContainerName {
			break
		}
	}

	path := fmt.Sprintf("/spec/containers/%d/env", i)
	var value interface{}
	if len(pod.Spec.Containers[i].Env) == 0 {
		value = []corev1.EnvVar{{Name: envName, Value: envValue}}
	} else {
		path += "/-"
		value = corev1.EnvVar{Name: envName, Value: envValue}
	}

	return patchOperation{Op: "add", Path: path, Value: value}
}

func addSparkConfigMap(pod *corev1.Pod, app *v1beta1.SparkApplication) []patchOperation {
	var patchOps []patchOperation
	sparkConfigMapName := app.Spec.SparkConfigMap
	if sparkConfigMapName != nil {
		patchOps = append(patchOps, addConfigMapVolume(pod, *sparkConfigMapName, config.SparkConfigMapVolumeName))
		patchOps = append(patchOps, addConfigMapVolumeMount(pod, config.SparkConfigMapVolumeName,
			config.DefaultSparkConfDir))
		patchOps = append(patchOps, addEnvironmentVariable(pod, config.SparkConfDirEnvVar, config.DefaultSparkConfDir))
	}
	return patchOps
}

func addHadoopConfigMap(pod *corev1.Pod, app *v1beta1.SparkApplication) []patchOperation {
	var patchOps []patchOperation
	hadoopConfigMapName := app.Spec.HadoopConfigMap
	if hadoopConfigMapName != nil {
		patchOps = append(patchOps, addConfigMapVolume(pod, *hadoopConfigMapName, config.HadoopConfigMapVolumeName))
		patchOps = append(patchOps, addConfigMapVolumeMount(pod, config.HadoopConfigMapVolumeName,
			config.DefaultHadoopConfDir))
		patchOps = append(patchOps, addEnvironmentVariable(pod, config.HadoopConfDirEnvVar, config.DefaultHadoopConfDir))
	}
	return patchOps
}

func addGeneralConfigMaps(pod *corev1.Pod, app *v1beta1.SparkApplication) []patchOperation {
	var configMaps []v1beta1.NamePath
	if util.IsDriverPod(pod) {
		configMaps = app.Spec.Driver.ConfigMaps
	} else if util.IsExecutorPod(pod) {
		configMaps = app.Spec.Executor.ConfigMaps
	}

	var patchOps []patchOperation
	for _, namePath := range configMaps {
		volumeName := namePath.Name + "-vol"
		if len(volumeName) > maxNameLength {
			volumeName = volumeName[0:maxNameLength]
			glog.V(2).Infof("ConfigMap volume name is too long. Truncating to length %d. Result: %s.", maxNameLength, volumeName)
		}
		patchOps = append(patchOps, addConfigMapVolume(pod, namePath.Name, volumeName))
		patchOps = append(patchOps, addConfigMapVolumeMount(pod, volumeName, namePath.Path))
	}
	return patchOps
}

func addConfigMapVolume(pod *corev1.Pod, configMapName string, configMapVolumeName string) patchOperation {
	volume := corev1.Volume{
		Name: configMapVolumeName,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: configMapName,
				},
			},
		},
	}
	return addVolume(pod, volume)
}

func addConfigMapVolumeMount(pod *corev1.Pod, configMapVolumeName string, mountPath string) patchOperation {
	mount := corev1.VolumeMount{
		Name:      configMapVolumeName,
		ReadOnly:  true,
		MountPath: mountPath,
	}
	return addVolumeMount(pod, mount)
}

func addAffinity(pod *corev1.Pod, app *v1beta1.SparkApplication) *patchOperation {
	var affinity *corev1.Affinity
	if util.IsDriverPod(pod) {
		affinity = app.Spec.Driver.Affinity
	} else if util.IsExecutorPod(pod) {
		affinity = app.Spec.Executor.Affinity
	}

	if affinity == nil {
		return nil
	}
	return &patchOperation{Op: "add", Path: "/spec/affinity", Value: *affinity}
}

func addTolerations(pod *corev1.Pod, app *v1beta1.SparkApplication) []patchOperation {
	var tolerations []corev1.Toleration
	if util.IsDriverPod(pod) {
		tolerations = app.Spec.Driver.Tolerations
	} else if util.IsExecutorPod(pod) {
		tolerations = app.Spec.Executor.Tolerations
	}

	var ops []patchOperation
	for _, v := range tolerations {
		ops = append(ops, addToleration(pod, v))
	}
	return ops
}

func addToleration(pod *corev1.Pod, toleration corev1.Toleration) patchOperation {
	path := "/spec/tolerations"
	var value interface{}
	if len(pod.Spec.Tolerations) == 0 {
		value = []corev1.Toleration{toleration}
	} else {
		path += "/-"
		value = toleration
	}

	return patchOperation{Op: "add", Path: path, Value: value}
}

func addSecurityContext(pod *corev1.Pod, app *v1beta1.SparkApplication) *patchOperation {
	var secContext *corev1.PodSecurityContext
	if util.IsDriverPod(pod) {
		secContext = app.Spec.Driver.SecurityContenxt
	} else if util.IsExecutorPod(pod) {
		secContext = app.Spec.Executor.SecurityContenxt
	}

	if secContext == nil {
		return nil
	}
	return &patchOperation{Op: "add", Path: "/spec/securityContext", Value: *secContext}
}
