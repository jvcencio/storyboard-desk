package main

import (
	"encoding/json"
	"fmt"
)

// cloudJobRule says what one job status means for the rules that protect the
// person's money. Every status a saved job can hold is in cloudJobRules, and
// code asks the rule instead of comparing status strings.
//
//	queued ─► submitting ─► pending ─► in_progress ─► downloading ─► importing ─► done
//	  │           │             └────────────┴──► failed
//	  ▼           ▼
//	blocked    unknown ──(the person confirms they checked)──► reconciled
//
//	A check that fails while a provider ID or a saved result exists gives
//	recoverable, which resumes. A job still queued at startup becomes cancelled.
type cloudJobRule struct {
	// unfinished: the provider may still hold work for this job, the app holds
	// a result it has not imported, or nobody has confirmed what happened. The
	// shot takes no second paid job until this one is resolved.
	unfinished bool
	// charges: until the provider reports a cost, the reservation counts
	// against the allowance.
	charges bool
	// final: nothing advances this job any more.
	final bool
}

var cloudJobRules = map[string]cloudJobRule{
	"queued":      {unfinished: true, charges: true}, // confirmed; the one paid request is not sent yet
	"submitting":  {unfinished: true, charges: true}, // the paid request is in flight
	"pending":     {unfinished: true, charges: true}, // the provider accepted a video job
	"in_progress": {unfinished: true, charges: true},
	"downloading": {unfinished: true, charges: true},
	"importing":   {unfinished: true, charges: true}, // the result is saved; the take is not in the project yet
	"recoverable": {unfinished: true, charges: true}, // a check failed; the provider ID or the saved result still stands
	"unknown":     {unfinished: true, charges: true}, // the request may have been sent; the person must reconcile it
	"reconciled":  {charges: true, final: true},      // the person checked provider activity; no cost was ever reported
	"failed":      {charges: true, final: true},      // the provider reported failure
	"done":        {charges: true, final: true},      // the take is in the project
	"blocked":     {final: true},                     // stopped before any request was sent
	"cancelled":   {final: true},                     // withdrawn before any request was sent
}

// cloudJobRuleFor reads a job's rule. A status this build does not know is
// read the careful way: unfinished and charged.
func cloudJobRuleFor(job ProjectDocument) cloudJobRule {
	if rule, ok := cloudJobRules[shotText(job, "status")]; ok {
		return rule
	}
	return cloudJobRule{unfinished: true, charges: true}
}

// cloneDocument returns a copy that shares nothing with the original, so a
// caller outside the service lock never reads a map the service is writing.
func cloneDocument(document ProjectDocument) ProjectDocument {
	var clone ProjectDocument
	_ = json.Unmarshal(rawValue(document), &clone)
	return clone
}

// Reconcile records that the person checked provider activity for a request
// whose outcome the app never learned. The shot can then take a new job. The
// reservation stays, because no cost was ever reported.
func (c *CloudService) Reconcile(jid string) (ProjectDocument, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.load(); err != nil {
		return nil, err
	}
	job, ok := c.jobs[jid]
	if !ok {
		return nil, fmt.Errorf("choose an existing cloud job")
	}
	if shotText(job, "status") != "unknown" {
		return nil, fmt.Errorf("only a request with an unknown outcome needs reconciling")
	}
	patch := ProjectDocument{"status": rawValue("reconciled"), "ended_at": rawValue(stamp()),
		"message": rawValue("You confirmed that you checked provider activity for this request. Its reservation stays, because no cost was reported.")}
	if err := c.update(job, patch); err != nil {
		return nil, err
	}
	return cloneDocument(job), nil
}
