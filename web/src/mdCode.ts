import rehypeHighlight from "rehype-highlight";
import { hlLangs } from "./hljs";

// Syntax highlighting for fenced code in replies; loaded the first time a reply has some.
export const rehypeCode: [[typeof rehypeHighlight, { languages: typeof hlLangs }]] = [[rehypeHighlight, { languages: hlLangs }]];
