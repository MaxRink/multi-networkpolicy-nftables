package controller

import (
	"fmt"
	"os"
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
	klog "k8s.io/klog/v2"
)

func PreparePodIptablesDir(path string) error {
	if path == "" {
		return nil
	}

	if _, err := os.Stat(path); err == nil {
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("delete pod iptables directory %q: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat pod iptables directory %q: %w", path, err)
	}

	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("create pod iptables directory %q: %w", path, err)
	}
	return nil
}

func (r *NodeReconciler) cleanupStalePodIptablesDirs(pods []corev1.Pod) {
	if r.PodIptables == "" {
		return
	}

	liveUIDs := make(map[string]struct{}, len(pods))
	for i := range pods {
		if pods[i].UID == "" {
			continue
		}
		liveUIDs[string(pods[i].UID)] = struct{}{}
	}

	entries, err := os.ReadDir(r.PodIptables)
	if err != nil {
		if !os.IsNotExist(err) {
			klog.Errorf("cannot list pod iptables directory %q: %v", r.PodIptables, err)
		}
		return
	}

	for _, entry := range entries {
		if _, ok := liveUIDs[entry.Name()]; ok {
			continue
		}
		path := filepath.Join(r.PodIptables, entry.Name())
		if err := os.RemoveAll(path); err != nil {
			klog.Errorf("cannot remove pod iptables dir(%s): %v", path, err)
		}
	}
}
