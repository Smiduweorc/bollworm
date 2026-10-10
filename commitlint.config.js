// Enforces Conventional Commits (https://www.conventionalcommits.org) so the
// history stays machine-readable and `npm run changelog` (git-cliff) can group
// commits automatically. Run on every commit via the lefthook commit-msg hook.
//
// This package is ESM ("type": "module"), so the config is an ES module.
export default { extends: ["@commitlint/config-conventional"] };
