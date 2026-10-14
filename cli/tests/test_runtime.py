from relay.commands.status import duration, runtime


def test_duration_formats():
    assert duration(45) == "45s"
    assert duration(192) == "3m12s"
    assert duration(3720) == "1h2m"


def test_runtime_finished_job():
    job = {
        "started_at": "2026-10-14T12:00:00Z",
        "finished_at": "2026-10-14T12:01:30Z",
    }
    assert runtime(job) == "1m30s"


def test_runtime_running_job_counts_from_start():
    job = {"started_at": "2026-10-14T12:00:00Z"}
    assert runtime(job) is not None


def test_runtime_never_started():
    assert runtime({"status": "queued"}) is None
