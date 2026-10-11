const fs = require('node:fs');
const {execFileSync} = require('node:child_process');
fs.copyFileSync('node_modules/htmx.org/dist/htmx.min.js', 'kodata/static/htmx.min.js');
fs.copyFileSync('node_modules/htmx.org/LICENSE', 'kodata/static/htmx.LICENSE');
execFileSync(process.execPath, ['node_modules/@tailwindcss/cli/dist/index.mjs', '-i', 'assets/styles.css', '-o', 'kodata/static/dashboard.css', '--minify'], {stdio: 'inherit'});
