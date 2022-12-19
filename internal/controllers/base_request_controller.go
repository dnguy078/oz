package controllers

import (
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/diranged/oz/internal/api/v1alpha1"
	"github.com/diranged/oz/internal/controllers/internal/status"
	"github.com/diranged/oz/internal/legacybuilder"
)

// BaseRequestReconciler provides a base reconciler with common functions for handling our Template CRDs
// (ExecAccessTemplate, AccessTemplate, etc)
type BaseRequestReconciler struct {
	BaseReconciler
}

// verifyDuration checks a few components of whether or not the AccessRequest is still valid:
//
//   - Was the (optional) supplied "spec.duration" valid?
//   - Is the target tempate "spec.defaultDuration"  valid?
//   - Is the target template "spec.maxDuration" valid?
//   - Did the user supply their own "spec.duration"?
//     yes? Is it lower than the target template "spec.maxDuration"?
//     no? Use the target template "spec.defaultDuration"
//   - Is the access request duration less than its current age?
//     yes? approve
//     no? mark the resource for deletion
func (r *BaseRequestReconciler) verifyDuration(builder legacybuilder.IBuilder) error {
	var err error
	logger := r.getLogger(builder.GetCtx())

	logger.Info("Beginning access request duration verification")

	// Step one - verify the inputs themselves. If the user supplied invalid inputs, or the template has any
	// invalid inputs, we bail out and update the conditions as such. This is to prevent escalated privilegess
	// from lasting indefinitely.
	var requestedDuration time.Duration
	if requestedDuration, err = builder.GetRequest().GetDuration(); err != nil {
		// NOTE: Blindly ignoring the error return here because we are already
		// returning an error which will fail the reconciliation.
		_ = status.SetRequestDurationsNotValid(builder.GetCtx(), r, builder.GetRequest(),
			fmt.Sprintf("spec.duration error: %s", err),
		)
		return err
	}
	templateDefaultDuration, err := builder.GetTemplate().GetAccessConfig().GetDefaultDuration()
	if err != nil {
		// NOTE: Blindly ignoring the error return here because we are already
		// returning an error which will fail the reconciliation.
		_ = status.SetRequestDurationsNotValid(builder.GetCtx(), r, builder.GetRequest(),
			fmt.Sprintf("Template Error, spec.defaultDuration error: %s", err),
		)
		return err
	}

	templateMaxDuration, err := builder.GetTemplate().GetAccessConfig().GetMaxDuration()
	if err != nil {
		// NOTE: Blindly ignoring the error return here because we are already
		// returning an error which will fail the reconciliation.
		_ = status.SetRequestDurationsNotValid(builder.GetCtx(), r, builder.GetRequest(),
			fmt.Sprintf("Template Error, spec.maxDuration error: %s", err),
		)
		return err
	}

	// Now determine which duration is the one we'll use
	var accessDuration time.Duration
	{
		var reasonStr string
		if requestedDuration == 0 {
			// If no requested duration supplied, then default to the template's default duration
			reasonStr = fmt.Sprintf(
				"Access request duration defaulting to template duration time (%s)",
				templateDefaultDuration.String(),
			)
			accessDuration = templateDefaultDuration
		} else if requestedDuration <= templateMaxDuration {
			// If the requested duration is too long, use the template max
			reasonStr = fmt.Sprintf("Access requested custom duration (%s)", requestedDuration.String())
			accessDuration = requestedDuration
		} else {
			// Finally, if it's valid, use the supplied duration
			reasonStr = fmt.Sprintf("Access requested duration (%s) larger than template maximum duration (%s)", requestedDuration.String(), templateMaxDuration.String())
			accessDuration = templateMaxDuration
		}

		// Log out the decision, and update the condition
		logger.Info(reasonStr)
		if err := status.SetRequestDurationsValid(builder.GetCtx(), r, builder.GetRequest(), reasonStr); err != nil {
			return err
		}
	}

	// If the accessUptime is greater than the accessDuration, kill it.
	if builder.GetRequest().GetUptime() > accessDuration {
		return status.SetAccessNotValid(builder.GetCtx(), r, builder.GetRequest())
	}

	// Update the resource, and let the user know how much time is remaining
	return status.SetAccessStillValid(builder.GetCtx(), r, builder.GetRequest())
}

// isAccessExpired checks the AccessRequest status for the ConditionAccessStillValid condition. If it is no longer
// a valid request, then the resource is immediately deleted.
//
// Returns:
//
//	true: if the resource is expired, AND has now been deleted
//	false: if the resource is still valid
//	error: any error during the checks
func (r *BaseRequestReconciler) isAccessExpired(builder legacybuilder.IBuilder) (bool, error) {
	logger := r.getLogger(builder.GetCtx())
	logger.Info("Checking if access has expired or not...")
	cond := meta.FindStatusCondition(
		*builder.GetRequest().GetStatus().GetConditions(),
		v1alpha1.ConditionAccessStillValid.String(),
	)
	if cond == nil {
		logger.Info(
			fmt.Sprintf(
				"Missing Condition %s, skipping deletion",
				v1alpha1.ConditionAccessStillValid,
			),
		)
		return false, nil
	}

	if cond.Status == metav1.ConditionFalse {
		logger.Info(
			fmt.Sprintf(
				"Found Condition %s in state %s, terminating rqeuest",
				v1alpha1.ConditionAccessStillValid,
				cond.Status,
			),
		)
		return true, r.DeleteResource(builder)
	}

	logger.Info(
		fmt.Sprintf(
			"Found Condition %s in state %s, leaving alone",
			v1alpha1.ConditionAccessStillValid,
			cond.Status,
		),
	)
	return false, nil
}

// verifyAccessResourcesBuilt calls out to the Builder interface's GenerateAccessResources() method to build out
// all of the resources that are required for thie particular access request. The Status.Conditions field is
// then updated with the ConditionAccessResourcesCreated condition appropriately.
func (r *BaseRequestReconciler) verifyAccessResourcesBuilt(
	builder legacybuilder.IBuilder,
) error {
	logger := log.FromContext(builder.GetCtx())
	logger.Info("Verifying that access resources are built")

	statusString, err := builder.GenerateAccessResources()
	if err != nil {
		// NOTE: Blindly ignoring the error return here because we are already
		// returning an error which will fail the reconciliation.
		_ = status.SetAccessResourcesNotCreated(builder.GetCtx(), r, builder.GetRequest(), err)
		return err
	}
	return status.SetAccessResourcesCreated(builder.GetCtx(), r, builder.GetRequest(), statusString)
}

// verifyAccessResourcesReady is a followup to the verifyAccessResources()
// function - where we make sure that the .Status.PodName resource has come all
// the way up and reached the "Running" phase.
func (r *BaseRequestReconciler) verifyAccessResourcesReady(
	builder legacybuilder.IPodAccessBuilder,
) error {
	logger := log.FromContext(builder.GetCtx())
	logger.Info("Verifying that access resources are ready")

	statusString, err := builder.VerifyAccessResources()
	if err != nil {
		// NOTE: Blindly ignoring the error return here because we are already
		// returning an error which will fail the reconciliation.
		_ = status.SetAccessResourcesNotReady(builder.GetCtx(), r, builder.GetRequest(), err)
		return err
	}

	return status.SetAccessResourcesReady(builder.GetCtx(), r, builder.GetRequest(), statusString)
}

// DeleteResource just deletes the resource immediately
//
// Returns:
//
//	error: Any error during the deletion
func (r *BaseRequestReconciler) DeleteResource(builder legacybuilder.IBuilder) error {
	return r.Delete(builder.GetCtx(), builder.GetRequest())
}