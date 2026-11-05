# Local frontend assets

The startup scripts, fonts and highlight styles are served locally and embedded
in production builds. License texts are in `THIRD_PARTY_NOTICES.txt`.

| Files | Upstream |
| --- | --- |
| react.production.min.js, react-dom.production.min.js | unpkg.com/react@18.3.1/umd/, unpkg.com/react-dom@18.3.1/umd/ |
| tailwind.js | https://cdn.tailwindcss.com/3.4.17 |
| marked.min.js | https://cdn.jsdelivr.net/npm/marked@15.0.12/marked.min.js |
| highlight.min.js, github*.css | https://cdnjs.cloudflare.com/ajax/libs/highlight.js/11.9.0/ |
| telegram-web-app.js | https://telegram.org/js/telegram-web-app.js (downloaded 2026-11-05) |
| manrope-latin-wght-normal.woff2 | @fontsource-variable/manrope@5.2.8/files/ |
| jetbrains-mono-latin-wght-normal.woff2 | @fontsource-variable/jetbrains-mono@5.2.8/files/ |

Keep the original upstream headers when updating these files. Mermaid continues
to load on demand; it is not a startup dependency.
