# Third-party notices

Colliedesk includes code from the projects below. Each notice is reproduced as its license asks.

## goldmark

`github.com/yuin/goldmark` (https://github.com/yuin/goldmark), used by the server to render help
center articles from Markdown.

```text
MIT License

Copyright (c) 2019 Yusuke Inuzuka

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

## Knowledge base editor (web, its own chunk)

The article editor (`web/src/features/kb/editor/`) is built on CodeMirror 6. Its chunk holds exactly
these packages (read from the build's source map):

| Package | Version | License | Copyright |
| --- | --- | --- | --- |
| `@codemirror/commands`, `@codemirror/language`, `@codemirror/search`, `@codemirror/state`, `@codemirror/view` | 6.10.1, 6.13.1, 6.7.2, 6.7.6, 6.43.15 | MIT | Copyright (C) 2018-2021 by Marijn Haverbeke and others |
| `@lezer/common`, `@lezer/highlight`, `@lezer/markdown` | 1.5.3, 1.2.5, 1.8.0 | MIT | Copyright (C) 2018, 2020 by Marijn Haverbeke and others |
| `style-mod`, `w3c-keyname`, `crelt`, `@marijn/find-cluster-break` | 4.1.4, 2.2.8, 1.0.7, 1.0.4 | MIT | Copyright (C) 2016-2024 by Marijn Haverbeke and others |

The preview beside it uses `react-markdown` and `remark-gfm` with the unified, mdast and micromark
packages they depend on (MIT, Copyright (c) Titus Wormer and the unified collective;
`escape-string-regexp`: Copyright (c) Sindre Sorhus).

Each of these packages is distributed under the MIT License:

```
Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
```

Each package's own `LICENSE` file (under `web/node_modules` after `pnpm install`) carries its exact copyright line.
