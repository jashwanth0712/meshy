#!/usr/bin/env python3
"""Build the static Meshy Insights mockups.

Reuses the ui-revamp mockup pipeline (docs/ui-revamp/mockups/build.py): same theme tokens,
component CSS, top nav, ⌘K palette and shell JS. Adds _src/_insights.css and _src/_insights.js
(chart marks, facet chips, tooltips) and points shared nav links back at the ui-revamp mockups.

    python3 docs/insights/mockups/build.py

Page sources live in _src/<page>.html with the same one-line JSON header as ui-revamp.
Links between insight pages are written as "./page.html" so they are not rewritten.
"""
import importlib.util
import re
from pathlib import Path

HERE = Path(__file__).resolve().parent
SRC = HERE / "_src"
UI = HERE.parents[1] / "ui-revamp" / "mockups"

spec = importlib.util.spec_from_file_location("uibuild", UI / "build.py")
ub = importlib.util.module_from_spec(spec)
spec.loader.exec_module(ub)

UI_REL = "../../ui-revamp/mockups/"
# Activity in the top nav opens the insights index; everything else goes to the ui-revamp pages.
ub.HREF = {k: ("./index.html" if k == "activity" else UI_REL + v) for k, v in ub.HREF.items()}
SHARED = "access|directory|billing|provider|settings|agent|give-access|revoke|approve|activity|index"


def build(page: Path) -> str:
    html = ub.render(page)
    # palette / lockup links that still point at ui-revamp page names
    html = re.sub(rf'href="({SHARED})\.html', lambda m: f'href="{UI_REL}{m.group(1)}.html', html)
    css = (SRC / "_insights.css").read_text()
    js = (SRC / "_insights.js").read_text()
    html = html.replace("</style>\n</head>", css + "\n</style>\n</head>", 1)
    html = html.replace("</body>", f"<script>\n{js}\n</script>\n</body>", 1)
    return html


def main():
    for p in sorted(p for p in SRC.glob("*.html") if not p.name.startswith("_")):
        (HERE / p.name).write_text(build(p))
        print("built", p.name)


if __name__ == "__main__":
    main()
