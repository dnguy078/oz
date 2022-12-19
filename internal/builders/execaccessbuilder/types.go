// Package execaccessbuilder implements the IBuilder interface for ExecAccessRequest resources
package execaccessbuilder

import (
	"github.com/diranged/oz/internal/builders"
)

//+kubebuilder:rbac:groups=crds.wizardofoz.co,resources=execaccessrequests,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=crds.wizardofoz.co,resources=execaccessrequests/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=crds.wizardofoz.co,resources=execaccessrequests/finalizers,verbs=update

// ExecAccessBuilder implements the IBuilder interface for ExecAccessRequest resources
type ExecAccessBuilder struct{}

// https://stackoverflow.com/questions/33089523/how-to-mark-golang-struct-as-implementing-interface
var (
	_ builders.IBuilder = &ExecAccessBuilder{}
	_ builders.IBuilder = (*ExecAccessBuilder)(nil)
)
