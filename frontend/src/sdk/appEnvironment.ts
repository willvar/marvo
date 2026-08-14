const MARVO_ANDROID_USER_AGENT_MARKER = 'MarvoAndroid/'

export function isMarvoAndroidApp(userAgent = typeof navigator === 'undefined' ? '' : navigator.userAgent) {
  return userAgent.includes(MARVO_ANDROID_USER_AGENT_MARKER)
}

export function androidRouteStorageKey(userID: string) {
  return `marvo.android.lastRoute.${userID}`
}
