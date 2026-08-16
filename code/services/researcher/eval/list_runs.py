#!/usr/bin/env python3
"""List evaluation runs from Google Cloud Vertex AI / Agent Platform."""

import os
import sys
from datetime import datetime
import click

try:
    import google.auth
    from google.auth.transport.requests import Request
    import requests
    from rich.console import Console
    from rich.table import Table
except ImportError as e:
    print(f"Missing required dependencies: {e}", file=sys.stderr)
    sys.exit(1)


@click.command()
@click.option("--project", default=None, help="GCP project ID or number.")
@click.option("--region", default="us-central1", help="GCP region.")
@click.option("--limit", default=20, help="Maximum number of runs to display.")
def list_eval_runs(project: str | None, region: str, limit: int) -> None:
    console = Console(width=160)

    # Resolve project ID
    resolved_project = project or os.environ.get("GOOGLE_CLOUD_PROJECT") or os.environ.get("PROJECT_ID")
    if not resolved_project:
        if os.path.exists(".env"):
            with open(".env", "r", encoding="utf-8") as f:
                for line in f:
                    if line.startswith("PROJECT_ID=") or line.startswith("GOOGLE_CLOUD_PROJECT="):
                        resolved_project = line.strip().split("=", 1)[1].strip('"\'')
                        break
    if not resolved_project:
        resolved_project = "70159681032"

    console.print(f"[bold cyan]Fetching evaluation runs for project [green]{resolved_project}[/green] in region [green]{region}[/green]...[/bold cyan]\n")

    try:
        credentials, _ = google.auth.default(scopes=["https://www.googleapis.com/auth/cloud-platform"])
        credentials.refresh(Request())
    except Exception as e:
        console.print(f"[bold red]Error acquiring Google Cloud credentials:[/bold red] {e}")
        sys.exit(1)

    url = f"https://{region}-aiplatform.googleapis.com/v1beta1/projects/{resolved_project}/locations/{region}/evaluationRuns?pageSize={limit}"
    headers = {"Authorization": f"Bearer {credentials.token}"}

    try:
        resp = requests.get(url, headers=headers)
        if resp.status_code != 200:
            console.print(f"[bold red]API Error ({resp.status_code}):[/bold red] {resp.text}")
            sys.exit(1)
        data = resp.json()
    except Exception as e:
        console.print(f"[bold red]Network error fetching runs:[/bold red] {e}")
        sys.exit(1)

    runs = data.get("evaluationRuns", [])
    if not runs:
        console.print("[yellow]No evaluation runs found in this project/region.[/yellow]")
        return

    table = Table(
        title="Agent Platform / Vertex AI Evaluation Runs",
        show_header=True,
        header_style="bold magenta",
    )
    table.add_column("Run ID (Use with `make eval-results RUN_ID=...`)", style="bold cyan", no_wrap=True)
    table.add_column("State", style="bold")
    table.add_column("Created", style="dim")
    table.add_column("Reasoning Engine / Labels", style="yellow")

    for r in runs:
        name = r.get("name", "")
        state = r.get("state", "UNKNOWN")
        create_time = r.get("createTime", "")
        if create_time:
            try:
                dt = datetime.fromisoformat(create_time.replace("Z", "+00:00"))
                formatted_time = dt.strftime("%Y-%m-%d %H:%M:%S")
            except Exception:
                formatted_time = create_time
        else:
            formatted_time = "-"

        state_style = "green" if state == "SUCCEEDED" else ("red" if state == "FAILED" else "yellow")
        state_display = f"[{state_style}]{state}[/{state_style}]"

        labels = r.get("labels", {})
        engine_id = labels.get("vertex-ai-evaluation-agent-engine-id", "")
        engine_str = f"Engine: {engine_id}" if engine_id else "-"

        table.add_row(name, state_display, formatted_time, engine_str)

    console.print(table)
    console.print("\n[dim]To inspect a run, copy the Run ID and run: [bold]make eval-results RUN_ID=<Run-ID>[/bold][/dim]\n")


if __name__ == "__main__":
    list_eval_runs()
