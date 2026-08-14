package cn.willvar.marvo

import android.content.ClipboardManager
import android.content.Context
import android.graphics.Color
import android.graphics.drawable.ColorDrawable
import android.os.Build
import android.os.Bundle
import android.view.Gravity
import android.view.KeyEvent
import android.view.WindowManager
import android.view.inputmethod.EditorInfo
import android.view.View
import android.widget.FrameLayout
import androidx.activity.OnBackPressedCallback
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatDialog
import androidx.core.content.edit
import androidx.core.view.ViewCompat
import androidx.core.view.WindowCompat
import androidx.core.view.updatePadding
import androidx.core.widget.doOnTextChanged
import androidx.appcompat.app.AppCompatActivity
import androidx.appcompat.app.AppCompatDelegate
import com.google.android.material.color.MaterialColors
import com.journeyapps.barcodescanner.ScanContract
import com.journeyapps.barcodescanner.ScanOptions
import cn.willvar.marvo.databinding.ActivityBindingBinding
import cn.willvar.marvo.databinding.DialogUserIdBinding
import okhttp3.Call
import okhttp3.Callback
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import java.io.IOException
import java.util.Locale
import java.util.UUID
import java.util.concurrent.Executors

class MainActivity : AppCompatActivity() {
    private val preferences by lazy { getSharedPreferences(PREFERENCES, Context.MODE_PRIVATE) }
    private val network = OkHttpClient()
    private val decoderExecutor = Executors.newSingleThreadExecutor()
    private var bindingCall: Call? = null
    private var bindingView: ActivityBindingBinding? = null
    private var webHost: MarvoWebHost? = null
    private var clipboardCheckPending = false
    private var manualUserID = ""
    private var manualUserIDDialog: AppCompatDialog? = null
    private lateinit var filePicker: AndroidFilePicker
    private lateinit var updateManager: AndroidUpdateManager
    private lateinit var permissionManager: AndroidPermissionManager
    private lateinit var downloadManager: AndroidDownloadManager

    private val scanLauncher =
        registerForActivityResult(ScanContract()) { result ->
            result.contents?.let(::acceptBindingValue)
        }

    private val galleryLauncher =
        registerForActivityResult(ActivityResultContracts.GetContent()) { uri ->
            uri ?: return@registerForActivityResult
            setBindingBusy(true, "正在识别二维码…")
            decoderExecutor.execute {
                val decoded = QRImageDecoder.decode(contentResolver, uri)
                runOnUiThread {
                    decoded.fold(::acceptBindingValue) {
                        showBindingError("没有在图片中识别到有效二维码")
                    }
                }
            }
        }

    override fun onCreate(savedInstanceState: Bundle?) {
        val storedPreferences = getSharedPreferences(PREFERENCES, Context.MODE_PRIVATE)
        if (storedPreferences.contains(KEY_DARK_MODE)) {
            delegate.localNightMode =
                if (storedPreferences.getBoolean(KEY_DARK_MODE, false)) {
                    AppCompatDelegate.MODE_NIGHT_YES
                } else {
                    AppCompatDelegate.MODE_NIGHT_NO
                }
        }
        super.onCreate(savedInstanceState)
        WindowCompat.setDecorFitsSystemWindows(window, false)
        filePicker = AndroidFilePicker(this)
        permissionManager = AndroidPermissionManager(this)
        updateManager = AndroidUpdateManager(this, network)
        downloadManager = AndroidDownloadManager(this, network, permissionManager)
        val userID = preferences.getString(KEY_USER_ID, null)
        if (userID != null && USER_ID_PATTERN.matches(userID)) {
            showWorkspace(userID)
        } else {
            showBinding()
        }
        updateManager.checkAtStartup()
        onBackPressedDispatcher.addCallback(
            this,
            object : OnBackPressedCallback(true) {
                override fun handleOnBackPressed() {
                    val host = webHost
                    if (host == null) {
                        moveTaskToBack(true)
                        return
                    }
                    host.handleBack { handled ->
                        if (!handled) moveTaskToBack(true)
                    }
                }
            },
        )
    }

    override fun onResume() {
        super.onResume()
        scheduleClipboardBindingCheck()
        webHost?.onResume()
        updateManager.onResume()
    }

    override fun onWindowFocusChanged(hasFocus: Boolean) {
        super.onWindowFocusChanged(hasFocus)
        if (hasFocus) consumeClipboardBindingCheck()
    }

    override fun onPause() {
        webHost?.onPause()
        super.onPause()
    }

    override fun onStop() {
        webHost?.onStop()
        super.onStop()
    }

    override fun onDestroy() {
        bindingCall?.cancel()
        bindingCall = null
        manualUserIDDialog?.dismiss()
        manualUserIDDialog = null
        webHost?.destroy()
        webHost = null
        filePicker.dispose()
        permissionManager.cancel()
        downloadManager.destroy()
        updateManager.destroy()
        decoderExecutor.shutdownNow()
        network.dispatcher.executorService.shutdown()
        network.connectionPool.evictAll()
        super.onDestroy()
    }

    private fun showBinding() {
        webHost?.destroy()
        webHost = null
        val view = ActivityBindingBinding.inflate(layoutInflater)
        bindingView = view
        setContentView(view.root)
        applySystemInsets(view.root)
        constrainBindingWidth(view)
        view.scanButton.setOnClickListener {
            val options =
                ScanOptions()
                    .setDesiredBarcodeFormats(ScanOptions.QR_CODE)
                    .setPrompt("扫描用户前台显示的二维码")
                    .setBeepEnabled(false)
                    .setOrientationLocked(false)
                    .setBarcodeImageEnabled(false)
            scanLauncher.launch(options)
        }
        view.galleryButton.setOnClickListener { galleryLauncher.launch("image/*") }
        view.manualUserIdButton.setOnClickListener { showManualUserIDDialog() }
    }

    private fun showWorkspace(userID: String) {
        bindingCall?.cancel()
        bindingCall = null
        clipboardCheckPending = false
        manualUserIDDialog?.dismiss()
        manualUserIDDialog = null
        bindingView = null
        webHost?.destroy()
        val container = FrameLayout(this)
        setContentView(container)
        applySystemInsets(container)
        webHost =
            MarvoWebHost(
                activity = this,
                container = container,
                userID = userID,
                localDeviceID = localDeviceID(),
                deviceName = deviceName(),
                picker = filePicker,
                permissionManager = permissionManager,
                updateManager = updateManager,
                downloads = downloadManager,
            ).also { it.start() }
    }

    private fun acceptBindingValue(raw: String) {
        val userID = raw.trim().lowercase(Locale.ROOT)
        if (!USER_ID_PATTERN.matches(userID)) {
            showBindingError("请输入有效的 20 位用户 ID")
            return
        }
        manualUserID = userID
        verifyUserSpace(userID)
    }

    private fun showManualUserIDDialog() {
        if (bindingCall != null || manualUserIDDialog != null) return
        val content = DialogUserIdBinding.inflate(layoutInflater)
        content.userIdInput.setText(manualUserID)
        content.userIdInput.setSelection(manualUserID.length)
        val dialog =
            AppCompatDialog(
                this,
                com.google.android.material.R.style.ThemeOverlay_Material3_MaterialAlertDialog,
            )
        dialog.setContentView(content.root)
        manualUserIDDialog = dialog
        dialog.setOnShowListener {
            content.connectButton.isEnabled = !content.userIdInput.text.isNullOrBlank()
            content.userIdInput.doOnTextChanged { text, _, _, _ ->
                manualUserID = text?.toString().orEmpty()
                content.userIdInputLayout.error = null
                content.connectButton.isEnabled = !text.isNullOrBlank()
            }
            content.cancelButton.setOnClickListener { dialog.dismiss() }
            content.connectButton.setOnClickListener { submitManualUserID(dialog, content) }
            content.userIdInput.setOnEditorActionListener { _, actionID, event ->
                val shouldSubmit =
                    actionID == EditorInfo.IME_ACTION_DONE ||
                        (event?.action == KeyEvent.ACTION_DOWN && event.keyCode == KeyEvent.KEYCODE_ENTER)
                if (!shouldSubmit) return@setOnEditorActionListener false
                submitManualUserID(dialog, content)
                true
            }
            dialog.window?.let { window ->
                val dialogWidth = minOf(resources.displayMetrics.widthPixels - dp(32), dp(480))
                window.setBackgroundDrawable(ColorDrawable(Color.TRANSPARENT))
                window.setSoftInputMode(WindowManager.LayoutParams.SOFT_INPUT_STATE_ALWAYS_VISIBLE)
                window.setDimAmount(0.32f)
                window.addFlags(WindowManager.LayoutParams.FLAG_DIM_BEHIND)
                window.attributes =
                    window.attributes.apply {
                        gravity = Gravity.CENTER
                        y = 0
                    }
                window.setLayout(dialogWidth, WindowManager.LayoutParams.WRAP_CONTENT)
            }
            content.userIdInput.requestFocus()
        }
        dialog.setOnDismissListener {
            if (manualUserIDDialog === dialog) manualUserIDDialog = null
        }
        dialog.show()
    }

    private fun submitManualUserID(dialog: AppCompatDialog, content: DialogUserIdBinding) {
        val userID = content.userIdInput.text?.toString()?.trim()?.lowercase(Locale.ROOT).orEmpty()
        if (!USER_ID_PATTERN.matches(userID)) {
            content.userIdInputLayout.error = getString(R.string.invalid_user_id)
            return
        }
        manualUserID = userID
        dialog.dismiss()
        verifyUserSpace(userID)
    }

    private fun scheduleClipboardBindingCheck() {
        val view = bindingView ?: return
        clipboardCheckPending = true
        view.root.post {
            if (hasWindowFocus()) consumeClipboardBindingCheck()
        }
    }

    private fun consumeClipboardBindingCheck() {
        if (!clipboardCheckPending) return
        clipboardCheckPending = false
        if (bindingView == null) return
        if (bindingCall != null) return
        val userID = clipboardUserID() ?: return
        manualUserID = userID
        verifyUserSpace(userID)
    }

    private fun clipboardUserID(): String? =
        runCatching {
            val clipboard = getSystemService(ClipboardManager::class.java)
            val clip = clipboard?.primaryClip ?: return@runCatching null
            if (clip.itemCount == 0) return@runCatching null
            clip.getItemAt(0).text?.toString()?.trim()?.lowercase(Locale.ROOT)
        }.getOrNull()?.takeIf(USER_ID_PATTERN::matches)

    private fun verifyUserSpace(userID: String) {
        setBindingBusy(true, "正在连接用户空间…")
        bindingCall?.cancel()
        val url =
            BuildConfig.SERVER_ORIGIN.toHttpUrl().newBuilder()
                .addPathSegments("api/user/$userID/auth/token")
                .addQueryParameter("local_device_id", localDeviceID())
                .build()
        val request =
            Request.Builder()
                .url(url)
                .header("User-Agent", appUserAgent())
                .get()
                .build()
        bindingCall =
            network.newCall(request).also { call ->
                call.enqueue(
                    object : Callback {
                        override fun onFailure(call: Call, error: IOException) {
                            if (call.isCanceled()) return
                            runOnUiThread {
                                bindingCall = null
                                showBindingError("无法连接 Marvo，请检查网络后重试")
                            }
                        }

                        override fun onResponse(call: Call, response: Response) {
                            response.use {
                                runOnUiThread {
                                    bindingCall = null
                                    when (response.code) {
                                        200 -> {
                                            preferences.edit { putString(KEY_USER_ID, userID) }
                                            showWorkspace(userID)
                                        }
                                        403 -> showBindingError("该用户空间当前不可用")
                                        404 -> showBindingError("没有找到这个用户空间")
                                        else -> showBindingError("用户空间验证失败，请稍后重试")
                                    }
                                }
                            }
                        }
                    },
                )
            }
    }

    private fun setBindingBusy(busy: Boolean, message: String = "") {
        val view = bindingView ?: return
        view.scanButton.isEnabled = !busy
        view.galleryButton.isEnabled = !busy
        view.manualUserIdButton.isEnabled = !busy
        view.bindingProgress.visibility = if (busy) View.VISIBLE else View.GONE
        view.bindingStatus.visibility = if (message.isBlank()) View.GONE else View.VISIBLE
        view.bindingStatus.text = message
        if (message.isNotBlank()) {
            view.bindingStatus.setTextColor(
                MaterialColors.getColor(view.bindingStatus, com.google.android.material.R.attr.colorOnSurfaceVariant),
            )
        }
    }

    private fun showBindingError(message: String) {
        setBindingBusy(false, message)
        val status = bindingView?.bindingStatus ?: return
        status.setTextColor(
            MaterialColors.getColor(
                status,
                android.R.attr.colorError,
            ),
        )
    }

    private fun localDeviceID(): String {
        val existing = preferences.getString(KEY_DEVICE_ID, null)
        if (!existing.isNullOrBlank()) return existing
        val created = UUID.randomUUID().toString()
        preferences.edit { putString(KEY_DEVICE_ID, created) }
        return created
    }

    private fun deviceName(): String {
        val manufacturer = safeHardwareName(Build.MANUFACTURER)
        val model = safeHardwareName(Build.MODEL)
        val hardware =
            when {
                model.isBlank() -> "Android"
                manufacturer.isBlank() || model.startsWith(manufacturer, ignoreCase = true) -> model
                else -> "$manufacturer $model"
            }
        return "Marvo · $hardware".take(50)
    }

    private fun safeHardwareName(value: String) = value.filterNot(Char::isISOControl).trim()

    internal fun syncNativeColorScheme(dark: Boolean) {
        if (preferences.contains(KEY_DARK_MODE) && preferences.getBoolean(KEY_DARK_MODE, false) == dark) return
        preferences.edit { putBoolean(KEY_DARK_MODE, dark) }
        delegate.localNightMode = if (dark) AppCompatDelegate.MODE_NIGHT_YES else AppCompatDelegate.MODE_NIGHT_NO
    }

    private fun appUserAgent() = "MarvoAndroid/${BuildConfig.VERSION_NAME}"

    private fun applySystemInsets(view: View) {
        val start = view.paddingStart
        val top = view.paddingTop
        val end = view.paddingEnd
        val bottom = view.paddingBottom
        ViewCompat.setOnApplyWindowInsetsListener(view) { target, insets ->
            val bars =
                insets.getInsets(
                    androidx.core.view.WindowInsetsCompat.Type.systemBars() or
                        androidx.core.view.WindowInsetsCompat.Type.ime(),
                )
            target.updatePadding(
                left = start + bars.left,
                top = top + bars.top,
                right = end + bars.right,
                bottom = bottom + bars.bottom,
            )
            insets
        }
        ViewCompat.requestApplyInsets(view)
    }

    private fun constrainBindingWidth(view: ActivityBindingBinding) {
        view.root.addOnLayoutChangeListener { root, _, _, _, _, _, _, _, _ ->
            val availableWidth = root.width - root.paddingLeft - root.paddingRight - dp(32)
            val targetWidth = minOf(availableWidth, dp(620)).coerceAtLeast(1)
            val params = view.bindingContent.layoutParams
            if (params.width != targetWidth) {
                params.width = targetWidth
                view.bindingContent.layoutParams = params
            }
        }
    }

    private fun dp(value: Int) = (value * resources.displayMetrics.density).toInt()

    private companion object {
        const val PREFERENCES = "marvo_android"
        const val KEY_USER_ID = "bound_user_id"
        const val KEY_DEVICE_ID = "local_device_id"
        const val KEY_DARK_MODE = "dark_mode"
        val USER_ID_PATTERN = Regex("[0-9a-f]{20}")
    }
}
