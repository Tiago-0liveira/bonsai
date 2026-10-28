// Package reviewpass defines bounded review: one review/fix pass, then typed
// checks, stopping at human review. It starts nothing. A Policy's existence
// grants no permission: only a Pass reserved with Authorization from an explicit
// local run confirmation may later be executed. Records hold no prompts, argv
// output, terminal output, environment values, tokens or remote prose.
package reviewpass
