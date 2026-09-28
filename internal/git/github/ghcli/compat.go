// Compatibility names preserve CLI/TUI behavior while the daemon and web use
// the context-aware domain interfaces. Implementation remains in core.
package ghcli

import core "github.com/Tiago-0liveira/bonsai/internal/core/gh"

type PR = core.PR
type TimelineItem = core.TimelineItem
type Review = core.Review
type Commit = core.Commit
type PRDetail = core.PRDetail
type Check = core.Check
type Run = core.Run
type Cache = core.Cache

var ListPRs = core.ListPRs
var ViewPR = core.ViewPR
var MergePR = core.MergePR
var MergePRStrategy = core.MergePRStrategy
var ClosePR = core.ClosePR
var ReopenPR = core.ReopenPR
var ReadyPR = core.ReadyPR
var ReviewPR = core.ReviewPR
var CreatePR = core.CreatePR
var Checks = core.Checks
var Rollup = core.Rollup
var ListMergedPRs = core.ListMergedPRs
var ListRuns = core.ListRuns
var NewCache = core.NewCache

const StateOpen = core.StateOpen
const StateMerged = core.StateMerged
const StateClosed = core.StateClosed
const ReviewApproved = core.ReviewApproved
const ReviewChangesRequested = core.ReviewChangesRequested
const DefaultCacheTTL = core.DefaultCacheTTL
