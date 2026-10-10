// Commitlint configuration for scbake.
// See docs/COMMIT_MESSAGES.md for the human-readable conventions.
//
// This file must be an ES module (.mjs): the commitlint GitHub Action does not
// support CommonJS `.js` config files.
export default {
  extends: ['@commitlint/config-conventional'],
  rules: {
    // Allow long URLs and fenced content in bodies/footers.
    'body-max-line-length': [0, 'always'],
    'footer-max-line-length': [0, 'always'],
  },
};
