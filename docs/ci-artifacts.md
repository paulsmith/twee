# CI replay artifacts

`twee run --script` can leave a replayable trace even when the scripted
scenario fails. Export that trace to a GIF and attach the GIF to the workflow
run so a reviewer can see the terminal state and input sequence without
reproducing the failure locally.

This example assumes an earlier workflow step has built `twee` and the
application under test. It intentionally does not enable `set -e`: the export
and upload steps must still run after the scenario fails.

```yaml
- name: Run scripted TUI scenario and render replay
  id: twee_scenario
  shell: bash
  run: |
    set -uo pipefail
    mkdir -p artifacts

    status=0
    twee run \
      --script testdata/checkout.json \
      --trace-out artifacts/checkout.twee \
      -- ./bin/myapp || status=$?

    # A trace may still be useful when the scripted scenario failed. Do not
    # hide a failed export: preserve its status if the scenario passed.
    if test -f artifacts/checkout.twee; then
      export_status=0
      twee export artifacts/checkout.twee \
        -o artifacts/checkout.gif \
        --input-overlay \
        --max-idle 2s || export_status=$?
      if test "$status" -eq 0 && test "$export_status" -ne 0; then
        status=$export_status
      fi
    else
      printf '%s\n' 'twee did not produce artifacts/checkout.twee' >&2
      test "$status" -ne 0 || status=1
    fi

    exit "$status"

- name: Upload TUI replay
  id: upload_twee_replay
  if: ${{ always() }}
  uses: actions/upload-artifact@v7
  with:
    name: tui-checkout-replay
    path: artifacts/checkout.gif
    if-no-files-found: warn
    retention-days: 14

- name: Link replay in job summary
  if: ${{ always() && steps.upload_twee_replay.outputs.artifact-url != '' }}
  shell: bash
  env:
    ARTIFACT_URL: ${{ steps.upload_twee_replay.outputs.artifact-url }}
  run: |
    printf '%s\n' "[Download the TUI replay](${ARTIFACT_URL})" >> "$GITHUB_STEP_SUMMARY"
```

The upload action exposes `artifact-url` only for a successfully created
artifact. The conditional summary step therefore does not add a broken link
when trace or export creation failed. The scenario step retains the scenario
failure in preference to a later export failure, while an export failure still
fails the job when the scenario itself passed.

## Treat traces as sensitive

The GIF is the default artifact because it is easy to review and exposes only
what the terminal renderer displayed. A raw `.twee` bundle can contain typed
input, terminal output, command arguments, and environment-derived metadata.
Do not upload raw traces to a broadly visible repository by default. If a team
needs the raw trace for diagnosis, upload it in a separate, explicitly
restricted artifact after reviewing its contents and choosing an appropriate
retention period.

`actions/upload-artifact@v7` makes the artifact URL available to later steps;
users must be signed in and the URL stops working when the artifact, run, or
repository is deleted. See the [upload-artifact action documentation](https://github.com/actions/upload-artifact)
for current inputs, retention limits, and output behavior.
