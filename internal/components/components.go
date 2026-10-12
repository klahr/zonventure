// Package components is the read-only registry of the build's
// pluggable seams — mailer, payments, blob storage, metrics exporter,
// roster importers, etc. — and which implementation each seam was
// wired to at startup.
//
// The point is observability, not configuration: swapping a seam's
// implementation means rebuilding the binary, so the GUI panel fed
// by this list is descriptive only.
// The slice is built once in cmd/server/main.go and passed through
// to the admin handlers; there is deliberately no global registry.
package components

// Component describes one wired-in implementation of a named seam.
type Component struct {
	// Name is the seam's canonical key — "mailer", "payments",
	// "metrics", etc. Stable across releases so the GUI can pin
	// label translations and operators can grep logs.
	Name string `json:"name"`
	// Impl is the active implementation's short name — "smtp",
	// "log", "stripe", "simulated", "prometheus". Multi-impl seams
	// (e.g. roster importers) join their list with ", ".
	Impl string `json:"impl"`
	// Enabled is false when the seam is compiled in but inert —
	// e.g. payments=stripe with no $ZV_STRIPE_SECRET_KEY, or
	// metrics=prometheus with $ZV_METRICS_ADDR unset. The GUI
	// uses it to render an "off" badge so operators can spot
	// missed configuration without reading docs.
	Enabled bool `json:"enabled"`
	// Notes is a one-liner the GUI shows under the impl label —
	// usually the env var to set, or a short reason the seam is
	// inert. Optional.
	Notes string `json:"notes,omitempty"`
}
