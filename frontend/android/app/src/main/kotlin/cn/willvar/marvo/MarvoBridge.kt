package cn.willvar.marvo

import org.json.JSONObject

internal enum class MarvoBridgeErrorCode {
    INVALID_ARGUMENT,
    BRIDGE_UNAVAILABLE,
    UNTRUSTED_ORIGIN,
    PERMISSION_DENIED,
    USER_CANCELLED,
    UNSUPPORTED,
    IO_ERROR,
}

internal class MarvoBridgeException(
    val code: MarvoBridgeErrorCode,
    override val message: String,
    val details: Any? = null,
) : Exception(message)

internal data class MarvoBridgeCall(
    val id: String,
    val method: String,
    val payload: JSONObject?,
)

internal object MarvoBridgeContract {
    private val noPayloadMethods =
        setOf(
            "env",
            "capabilities",
            "haptic",
            "backToHome",
            "exitApp",
            "checkUpdate",
        )
    private val mimeType = Regex("[A-Za-z0-9!#$%&'*+.^_`|~-]+/[A-Za-z0-9!#$%&'*+.^_`|~-]+")

    fun validate(raw: String): MarvoBridgeCall {
        val request =
            try {
                JSONObject(raw)
            } catch (_: Throwable) {
                throw MarvoBridgeException(MarvoBridgeErrorCode.INVALID_ARGUMENT, "Malformed bridge request")
            }
        val id = request.nonEmptyString("id", 128)
        val method = request.nonEmptyString("method", 64)
        val rawPayload = request.opt("payload").takeUnless { it == null || it == JSONObject.NULL }
        val payload = rawPayload as? JSONObject
        if (rawPayload != null && payload == null) {
            throw MarvoBridgeException(MarvoBridgeErrorCode.INVALID_ARGUMENT, "payload must be an object or null")
        }
        when (method) {
            in noPayloadMethods -> requireNoPayload(payload, method)
            "toast" -> validateToast(payload)
            "statusBar" -> validateStatusBar(payload)
            "saveImage" -> validateSaveImage(payload)
            "share" -> validateShare(payload)
            else -> throw MarvoBridgeException(
                MarvoBridgeErrorCode.INVALID_ARGUMENT,
                "Unknown bridge method: $method",
            )
        }
        return MarvoBridgeCall(id, method, payload)
    }

    fun requestID(raw: String): String =
        runCatching { JSONObject(raw).optString("id").takeIf(String::isNotBlank) ?: "invalid" }
            .getOrDefault("invalid")

    fun success(id: String, result: Any? = null): String =
        JSONObject()
            .put("id", id)
            .put("ok", true)
            .put("result", JSONObject.wrap(result))
            .toString()

    fun failure(id: String, error: Throwable): String {
        val bridge = error as? MarvoBridgeException
        return failure(
            id,
            bridge?.code ?: MarvoBridgeErrorCode.IO_ERROR,
            bridge?.message ?: error.message ?: "Native operation failed",
            bridge?.details,
        )
    }

    fun failure(
        id: String,
        code: MarvoBridgeErrorCode,
        message: String,
        details: Any? = null,
    ): String =
        JSONObject()
            .put("id", id)
            .put("ok", false)
            .put(
                "error",
                JSONObject()
                    .put("code", code.name)
                    .put("message", message)
                    .put("details", JSONObject.wrap(details)),
            )
            .toString()

    private fun requireNoPayload(payload: JSONObject?, method: String) {
        if (payload != null) {
            throw MarvoBridgeException(MarvoBridgeErrorCode.INVALID_ARGUMENT, "$method does not accept parameters")
        }
    }

    private fun validateToast(payload: JSONObject?) {
        val body = payload.required()
        body.nonEmptyString("message", 4_000)
        val duration = body.optionalString("duration") ?: "short"
        if (duration !in setOf("short", "long")) {
            throw MarvoBridgeException(MarvoBridgeErrorCode.INVALID_ARGUMENT, "duration must be short or long")
        }
    }

    private fun validateStatusBar(payload: JSONObject?) {
        val style = payload.required().nonEmptyString("style", 16)
        if (style !in setOf("dark", "light")) {
            throw MarvoBridgeException(MarvoBridgeErrorCode.INVALID_ARGUMENT, "style must be dark or light")
        }
    }

    private fun validateSaveImage(payload: JSONObject?) {
        val body = payload.required()
        body.nonEmptyString("data", MAX_ENCODED_FILE_LENGTH)
        body.nonEmptyString("filename", 200)
    }

    private fun validateShare(payload: JSONObject?) {
        val body = payload.required()
        val text = body.optionalString("text")
        if (text != null && text.length > 100_000) {
            throw MarvoBridgeException(MarvoBridgeErrorCode.INVALID_ARGUMENT, "share text is too long")
        }
        val rawFile = body.opt("file").takeUnless { it == null || it == JSONObject.NULL }
        val file = rawFile as? JSONObject
        if (rawFile != null && file == null) {
            throw MarvoBridgeException(MarvoBridgeErrorCode.INVALID_ARGUMENT, "file must be an object")
        }
        file?.let {
            it.nonEmptyString("data", MAX_ENCODED_FILE_LENGTH)
            it.nonEmptyString("filename", 200)
            val type = it.nonEmptyString("mimeType", 120)
            if (!mimeType.matches(type)) {
                throw MarvoBridgeException(MarvoBridgeErrorCode.INVALID_ARGUMENT, "mimeType is invalid")
            }
        }
        if (text.isNullOrEmpty() && file == null) {
            throw MarvoBridgeException(MarvoBridgeErrorCode.INVALID_ARGUMENT, "share requires text or one file")
        }
    }

    private fun JSONObject?.required() =
        this ?: throw MarvoBridgeException(MarvoBridgeErrorCode.INVALID_ARGUMENT, "payload must be an object")

    private fun JSONObject.nonEmptyString(name: String, maxLength: Int): String {
        val value = opt(name) as? String
        if (value.isNullOrBlank() || value.length > maxLength) {
            throw MarvoBridgeException(
                MarvoBridgeErrorCode.INVALID_ARGUMENT,
                "$name must be a non-empty string no longer than $maxLength characters",
            )
        }
        return value
    }

    private fun JSONObject.optionalString(name: String): String? {
        if (!has(name) || opt(name) == JSONObject.NULL) return null
        return opt(name) as? String
            ?: throw MarvoBridgeException(MarvoBridgeErrorCode.INVALID_ARGUMENT, "$name must be a string")
    }

    private const val MAX_ENCODED_FILE_LENGTH = 48 * 1024 * 1024
}

