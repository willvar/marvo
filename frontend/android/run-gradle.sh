#!/bin/sh
set -eu

android_script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

java_major() {
  "$1/bin/java" -XshowSettings:properties -version 2>&1 |
    sed -n 's/^[[:space:]]*java.version = \([0-9][0-9]*\).*/\1/p' |
    head -n 1
}

supported_jdk() {
  [ -x "$1/bin/java" ] || return 1
  android_java_major=$(java_major "$1")
  [ "$android_java_major" = "17" ] || [ "$android_java_major" = "21" ]
}

android_java_home=
if [ -n "${ANDROID_JAVA_HOME:-}" ]; then
  if ! supported_jdk "$ANDROID_JAVA_HOME"; then
    echo "ANDROID_JAVA_HOME must point to JDK 17 or 21" >&2
    exit 1
  fi
  android_java_home=$ANDROID_JAVA_HOME
elif [ -n "${JAVA_HOME:-}" ] && supported_jdk "$JAVA_HOME"; then
  android_java_home=$JAVA_HOME
else
  for android_jdk_candidate in \
    /usr/lib/jvm/java-21-openjdk \
    /usr/lib/jvm/java-17-openjdk \
    /opt/android-studio/jbr \
    "${HOME:-/nonexistent}"/.gradle/jdks/*-21-* \
    "${HOME:-/nonexistent}"/.gradle/jdks/*-17-* \
    "${HOME:-/nonexistent}"/.jdks/*; do
    if supported_jdk "$android_jdk_candidate"; then
      android_java_home=$android_jdk_candidate
      break
    fi
  done
fi

if [ -z "$android_java_home" ]; then
  echo "Android builds require JDK 17 or 21; set ANDROID_JAVA_HOME=/path/to/jdk" >&2
  exit 1
fi

android_sdk_home=${ANDROID_HOME:-${ANDROID_SDK_ROOT:-}}
if [ -z "$android_sdk_home" ]; then
  for android_sdk_candidate in \
    "${HOME:-/nonexistent}/Android/Sdk" \
    "${HOME:-/nonexistent}/Library/Android/sdk" \
    /opt/android-sdk; do
    if [ -d "$android_sdk_candidate/platforms/android-36" ]; then
      android_sdk_home=$android_sdk_candidate
      break
    fi
  done
fi

if [ ! -d "$android_sdk_home/platforms/android-36" ]; then
  echo "Android SDK 36 was not found; set ANDROID_HOME=/path/to/sdk" >&2
  exit 1
fi

exec env JAVA_HOME="$android_java_home" ANDROID_HOME="$android_sdk_home" "$android_script_dir/gradlew" "$@"
