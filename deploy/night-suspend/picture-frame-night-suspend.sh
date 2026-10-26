#!/usr/bin/env bash
# Suspend the frame until the next morning. Installed as
# /usr/local/sbin/picture-frame-night-suspend and started by
# picture-frame-night-suspend.timer.
#
# The timer fires every 30 minutes inside the night window, so a failed or
# inhibited suspend gets another chance. The RTC alarm is armed *before* the
# suspend; without a working alarm the script refuses to suspend.
set -euo pipefail

# Read schedule from /etc/default/picture-frame-night-suspend (written at
# install time from the Go config; can be edited directly or via the web UI
# after a restart).
CONF=/etc/default/picture-frame-night-suspend
# shellcheck source=/dev/null
[[ -r $CONF ]] && . "$CONF"

WEEKDAY_SUSPEND_START=${WEEKDAY_SUSPEND_START:-${SUSPEND_START:-23:00}}
WEEKDAY_SUSPEND_END=${WEEKDAY_SUSPEND_END:-${SUSPEND_END:-06:30}}
WEEKDAY_WAKE_AT=${WEEKDAY_WAKE_AT:-${WAKE_AT:-07:00}}

WEEKEND_SUSPEND_START=${WEEKEND_SUSPEND_START:-${SUSPEND_START:-23:00}}
WEEKEND_SUSPEND_END=${WEEKEND_SUSPEND_END:-${SUSPEND_END:-08:30}}
WEEKEND_WAKE_AT=${WEEKEND_WAKE_AT:-${WAKE_AT:-09:00}}

RTC=${RTC:-/dev/rtc0}
RTWAKE=${RTWAKE:-/usr/sbin/rtcwake}

log() { logger -t picture-frame-night-suspend -- "$*"; }

to_minutes() {
    local hh=${1%%:*} mm=${1##*:}
    echo $((10#$hh * 60 + 10#$mm))
}

get_schedule() {
    local spec=$1
    local dow
    dow=$(date -d "$spec" +%u) # 1=Mon .. 7=Sun
    if ((dow == 6 || dow == 7)); then
        echo "$WEEKEND_SUSPEND_START $WEEKEND_SUSPEND_END $WEEKEND_WAKE_AT"
    else
        echo "$WEEKDAY_SUSPEND_START $WEEKDAY_SUSPEND_END $WEEKDAY_WAKE_AT"
    fi
}

now_minutes=$((10#$(date +%H) * 60 + 10#$(date +%M)))
wake_at=""

# 1. Check if Today's schedule window is currently active.
read -r today_start today_end today_wake <<< "$(get_schedule "today")"
t_start_min=$(to_minutes "$today_start")
t_end_min=$(to_minutes "$today_end")

if ((t_start_min < t_end_min)); then
    if ((now_minutes >= t_start_min && now_minutes <= t_end_min)); then
        wake_at=$today_wake
    fi
else
    if ((now_minutes >= t_start_min)); then
        wake_at=$today_wake
    fi
fi

# 2. If not active today, check if Yesterday's overnight window crosses into today.
if [[ -z $wake_at ]]; then
    read -r yest_start yest_end yest_wake <<< "$(get_schedule "yesterday")"
    y_start_min=$(to_minutes "$yest_start")
    y_end_min=$(to_minutes "$yest_end")

    if ((y_start_min >= y_end_min && now_minutes <= y_end_min)); then
        wake_at=$yest_wake
    fi
fi

# If outside any suspend window, exit cleanly.
[[ -n $wake_at ]] || exit 0

wake_epoch=$(date -d "today $wake_at" +%s)
now_epoch=$(date +%s)
if ((wake_epoch <= now_epoch + 60)); then
    wake_epoch=$(date -d "tomorrow $wake_at" +%s)
fi

# Replace any stale alarm, then arm the wake time. If this fails, do not
# suspend: there would be no automatic way back.
"$RTWAKE" -d "$RTC" -m disable >/dev/null 2>&1 || true
"$RTWAKE" -d "$RTC" -m no -t "$wake_epoch" >/dev/null

log "suspending; wake alarm at $(date -d "@$wake_epoch" '+%F %T %Z')"
if ! systemctl suspend; then
    "$RTWAKE" -d "$RTC" -m disable >/dev/null 2>&1 || true
    log "suspend failed; cleared wake alarm"
    exit 1
fi
# Do not clear the alarm here: systemctl may return as soon as the suspend is
# queued, and the kernel consumes the alarm on wake. Clearing it here could
# cancel the wake-up before the system actually suspends.
log "suspend requested"