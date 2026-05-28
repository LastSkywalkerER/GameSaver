# Protocol — Proof loop

## 🔴 Claude does not confirm its own work

"Should work", "builds fine", "this fixes it" — without an artifact — is **invalid**. Every claim needs
proof appropriate to the claim:

| Claim | Required proof |
|---|---|
| "It compiles" | `wails build` exit 0 (paste the `Built '...'` line). |
| "It's shipped" | CI run concluded `success` + `releases/tags/vX.Y.Z` returns the tag. |
| "The struct is the right size" | `unsafe.Sizeof` vs the documented C `sizeof`. |
| "The method is exposed to the frontend" | grep the regenerated `frontend/wailsjs/go/main/App.d.ts`. |
| "The secret isn't in the repo" | grep; and a clean push (no push-protection block). |

## What Claude CANNOT prove (and must hand to the user)

GameSaver's hardest bugs are **runtime behavior on the user's hardware**: does the game see the
controller, does sleep stay asleep, does the picker show on the lit monitor, does the game launch in
front. Claude cannot observe these. So:

- State clearly what was verified (build/CI) vs what needs the user to test.
- Give the user the **specific thing to try** ("launch a GOG game with the controller → it should come to front and the pad should work in-game").
- When the user reports it still broke, treat their report as the ground truth and switch to [`structured-reasoning.md`](structured-reasoning.md) — don't re-assert that it "should" work.

## Trust but verify (Claude's own edits)

After editing, the edit tool confirms the diff applied; that is not proof the *change is correct*.
Re-read the surrounding code if the change interacts with magic numbers or event loops.
