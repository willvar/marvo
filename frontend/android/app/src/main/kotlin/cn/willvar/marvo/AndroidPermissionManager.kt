package cn.willvar.marvo

import android.content.pm.PackageManager
import androidx.activity.ComponentActivity
import androidx.activity.result.contract.ActivityResultContracts
import androidx.core.content.ContextCompat

internal class AndroidPermissionManager(
    private val activity: ComponentActivity,
) {
    private var callback: ((Boolean) -> Unit)? = null
    private val launcher =
        activity.registerForActivityResult(ActivityResultContracts.RequestMultiplePermissions()) { result ->
            val pending = callback
            callback = null
            pending?.invoke(result.isNotEmpty() && result.values.all { it })
        }

    fun request(
        permissions: Collection<String>,
        result: (Boolean) -> Unit,
    ) {
        val required =
            permissions.distinct().filter {
                ContextCompat.checkSelfPermission(activity, it) != PackageManager.PERMISSION_GRANTED
            }
        if (required.isEmpty()) {
            result(true)
            return
        }
        callback?.invoke(false)
        callback = result
        runCatching { launcher.launch(required.toTypedArray()) }
            .onFailure {
                callback = null
                result(false)
            }
    }

    fun cancel() {
        callback?.invoke(false)
        callback = null
    }
}
