A decision model answers typed questions about something you show it, with probabilities instead of text, in well under a second and for a fraction of what one of your own turns costs. You reach it two ways, with the same arguments. Call the `decide` tool for a judgment or two in the middle of a task: is this the right file, which of these does the human mean, does this result answer the question. Use `silo_runtime.decide` from Python when code has to branch on a judgment many times over: classify or route a batch of items, keep the search results or files that are relevant, check each row against a rule, score candidates. Either beats reading every item yourself, and a number you can threshold beats your own guess.

```python
import silo_runtime as sr
out = sr.decide(
    {"ticket": text},                     # state: what to judge, as named fields
    {
        "is_bug":  {"type": "noul", "instructions": "Is `ticket` reporting a defect?"},
        "team":    {"type": "choice", "instructions": "Which team owns it?",
                    "criteria": {"payments": "checkout, billing", "frontend": "rendering, layout"}},
        "urgency": {"type": "score", "instructions": "How urgent is it?",
                    "criteria": ["can wait", "this week", "blocking now"]},
    },
)
a = out["answers"]
a["is_bug"]["noul"]       # probability of yes, 0 to 1
a["team"]["choice"]       # the option picked; also ["probabilities"] and ["confidence"]
a["urgency"]["score"]     # position on the scale, from 0; also ["confidence"]
```

`noul` is yes or no, `choice` picks one of the options you describe, `score` rates against 2 to 10 ordered levels. One call takes up to 200 questions about the same state and costs about what one does, so ask them together; for many items, make one call per item from threads. The numbers are calibrated: act on 0.9 and above (0.1 and below for a no) and treat the middle as "not sure" — look yourself, or ask the human. It judges only what is in `state`: it cannot search, calculate, or explain itself. The default model is `{default}`; `model=` takes another of: {models}.
