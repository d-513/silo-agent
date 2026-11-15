// Draws the looks export.py asks for: a JSON list of looks on stdin, their SVG
// documents as a JSON list on stdout. The drawing itself is the web's.
import { renderStack } from "../web/src/stack.ts";

let text = "";
process.stdin.setEncoding("utf8");
for await (const chunk of process.stdin) text += chunk;
process.stdout.write(JSON.stringify(JSON.parse(text).map(renderStack)));
