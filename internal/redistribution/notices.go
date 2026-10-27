// Package redistribution carries upstream redistribution texts in the portable
// executable. Regenerate from verified module sources with make build.
package redistribution

import _ "embed"

// Text contains the selected upstream notices, not a legal clearance claim.
//
//go:embed THIRD-PARTY-NOTICES.txt
var Text string
