package cn.willvar.marvo

import android.content.res.Configuration
import android.graphics.Color
import androidx.activity.ComponentActivity
import androidx.appcompat.app.AppCompatDelegate
import androidx.core.view.WindowCompat

internal enum class NativeColorSchemePreference(
    val wireValue: String,
    val nightMode: Int,
) {
    SYSTEM("system", AppCompatDelegate.MODE_NIGHT_FOLLOW_SYSTEM),
    LIGHT("light", AppCompatDelegate.MODE_NIGHT_NO),
    DARK("dark", AppCompatDelegate.MODE_NIGHT_YES),
    ;

    companion object {
        fun fromWire(value: String?): NativeColorSchemePreference? = entries.find { it.wireValue == value }
    }
}

internal object NativeColorScheme {
    fun isDark(configuration: Configuration): Boolean =
        configuration.uiMode and Configuration.UI_MODE_NIGHT_MASK == Configuration.UI_MODE_NIGHT_YES

    fun background(dark: Boolean): Int = Color.parseColor(if (dark) DARK_BACKGROUND else LIGHT_BACKGROUND)

    // Marvo does not opt into edge-to-edge yet, so these setters remain the direct way to keep
    // the system bars aligned with the embedded app palette on every supported Android version.
    @Suppress("DEPRECATION")
    fun applySystemBars(
        activity: ComponentActivity,
        dark: Boolean,
    ) {
        val background = background(dark)
        activity.window.statusBarColor = background
        activity.window.navigationBarColor = background
        WindowCompat.getInsetsController(activity.window, activity.window.decorView).apply {
            isAppearanceLightStatusBars = !dark
            isAppearanceLightNavigationBars = !dark
        }
    }

    private const val DARK_BACKGROUND = "#1a1b1e"
    private const val LIGHT_BACKGROUND = "#ffffff"
}
