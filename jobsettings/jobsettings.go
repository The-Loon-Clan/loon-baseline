// Package jobsettings is the persistence backend for loon's admin-editable
// job/service config variables (schedule.JobConfigVar). A service declares its
// variables in code via JobInfo.DeclareConfig(store, vars...); the *values* an
// operator overrides live here, in a job_settings table keyed by (job_name,
// key). The bundled admin config page (schedule.JobConfigHandler) reads and
// writes through this store.
//
// It exists in loon-baseline rather than loon because loon core deliberately
// owns no application tables — the schedule package declares the
// JobConfigStore interface, and this is the reference Postgres implementation
// a host wires in. A host with its own settings table implements the interface
// directly and ignores this package.
//
// Because settings live in one shared table keyed by job name, a value set on
// one process (the web admin) is read by another process that registered the
// same-named service (e.g. the loon-api read tier reading its cache TTL). That
// is the cross-process settings path behind LOON-DISTRIBUTED: declare a
// service in the web with MarkRemote for its config, edit it in the web admin,
// read it from the remote tier.
package jobsettings

import (
	"context"

	"github.com/the-loon-clan/loon/schedule"
)

// Store persists per-job config overrides. Its method set is identical to
// schedule.JobConfigStore by design, so a *PGStore is accepted directly wherever
// the scheduler wants a JobConfigStore (asserted in pgstore.go).
type Store interface {
	// GetJobSettings returns every override for one job as key->value. A job
	// with no overrides yields an empty (non-nil) map, not an error.
	GetJobSettings(ctx context.Context, jobName string) (map[string]string, error)
	// SetJobSetting upserts one override. An empty value deletes it (so the
	// declared default applies again) — matching schedule's convention.
	SetJobSetting(ctx context.Context, jobName, key, value string) error
	// DeleteJobSetting removes one override, reverting to the declared default.
	DeleteJobSetting(ctx context.Context, jobName, key string) error
}

// compile-time guarantee that the interface stays a drop-in for the scheduler.
var _ schedule.JobConfigStore = (Store)(nil)
