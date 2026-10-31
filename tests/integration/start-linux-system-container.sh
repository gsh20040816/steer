#!/bin/sh
set -eu

[ "$#" -eq 2 ] || {
	echo "usage: start-linux-system-container.sh <source-root> <artifacts-root>" >&2
	exit 2
}

source_root=$1
artifacts_root=$2
container_name=steer-linux-system
image_name=steer-linux-system:test
max_start_attempts=3
readiness_checks=30

diagnose_startup() {
	echo "Linux system container startup attempt $start_attempt/$max_start_attempts failed." >&2
	docker inspect --format 'status={{.State.Status}} running={{.State.Running}} exit={{.State.ExitCode}} error={{.State.Error}}' "$container_name" >&2 || true
	docker logs "$container_name" >&2 || true
	if [ "$(docker inspect --format '{{.State.Running}}' "$container_name" 2>/dev/null || true)" = true ]; then
		docker exec "$container_name" systemctl is-system-running >&2 || true
		docker exec "$container_name" systemctl --failed --no-pager >&2 || true
		docker exec "$container_name" journalctl -b --no-pager -n 200 >&2 || true
	fi
}

start_attempt=1
while [ "$start_attempt" -le "$max_start_attempts" ]; do
	docker container rm --force "$container_name" >/dev/null 2>&1 || true
	if docker run --detach \
		--name "$container_name" \
		--privileged \
		--cgroupns=host \
		--tmpfs /run \
		--tmpfs /run/lock \
		--volume /sys/fs/cgroup:/sys/fs/cgroup:rw \
		--volume "$source_root:/workspace:ro" \
		--volume "$artifacts_root:/artifacts:ro" \
		"$image_name" >/dev/null
	then
		readiness_check=1
		while [ "$readiness_check" -le "$readiness_checks" ]; do
			if [ "$(docker inspect --format '{{.State.Running}}' "$container_name" 2>/dev/null || true)" != true ]; then
				break
			fi
			systemd_state=$(docker exec "$container_name" systemctl is-system-running 2>/dev/null || true)
			case "$systemd_state" in
				running|degraded)
					if docker exec "$container_name" systemctl is-active --quiet systemd-resolved; then
						echo "Linux system container became ready on startup attempt $start_attempt/$max_start_attempts."
						exit 0
					fi
					;;
			esac
			readiness_check=$((readiness_check + 1))
			sleep 1
		done
	fi

	diagnose_startup
	docker container rm --force "$container_name" >/dev/null 2>&1 || true
	if [ "$start_attempt" -ge "$max_start_attempts" ]; then
		echo "Linux system container did not become ready after $max_start_attempts attempts." >&2
		exit 1
	fi
	start_attempt=$((start_attempt + 1))
	sleep 2
done
