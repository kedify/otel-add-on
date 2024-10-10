package util

import (
	"github.com/kedacore/keda/v2/pkg/scalers/externalscaler"
	"k8s.io/apimachinery/pkg/types"
)

func NamespacedNameFromScaledObjectRef(sor *externalscaler.ScaledObjectRef) *types.NamespacedName {
	if sor == nil {
		return nil
	}

	return &types.NamespacedName{
		Namespace: sor.GetNamespace(),
		Name:      sor.GetName(),
	}
}
