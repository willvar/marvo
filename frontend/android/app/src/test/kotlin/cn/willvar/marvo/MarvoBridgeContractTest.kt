package cn.willvar.marvo

import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertThrows
import org.junit.Test

class MarvoBridgeContractTest {
    @Test
    fun acceptsKnownNoPayloadMethod() {
        val call = MarvoBridgeContract.validate(request("env"))

        assertEquals("request-1", call.id)
        assertEquals("env", call.method)
        assertNull(call.payload)
    }

    @Test
    fun rejectsPayloadForNoPayloadMethod() {
        val error =
            assertThrows(MarvoBridgeException::class.java) {
                MarvoBridgeContract.validate(request("env", JSONObject().put("unexpected", true)))
            }

        assertEquals(MarvoBridgeErrorCode.INVALID_ARGUMENT, error.code)
    }

    @Test
    fun rejectsUnknownMethod() {
        val error =
            assertThrows(MarvoBridgeException::class.java) {
                MarvoBridgeContract.validate(request("runArbitraryNativeCode"))
            }

        assertEquals(MarvoBridgeErrorCode.INVALID_ARGUMENT, error.code)
    }

    @Test
    fun validatesStatusBarValues() {
        val accepted =
            MarvoBridgeContract.validate(
                request("statusBar", JSONObject().put("style", "dark")),
            )
        assertEquals("dark", accepted.payload?.getString("style"))

        assertThrows(MarvoBridgeException::class.java) {
            MarvoBridgeContract.validate(
                request("statusBar", JSONObject().put("style", "transparent")),
            )
        }
    }

    @Test
    fun validatesColorSchemePreferenceAndResolvedValue() {
        val system =
            MarvoBridgeContract.validate(
                request(
                    "colorScheme",
                    JSONObject().put("preference", "system").put("resolved", "dark"),
                ),
            )
        assertEquals("system", system.payload?.getString("preference"))
        assertEquals("dark", system.payload?.getString("resolved"))

        val explicit =
            MarvoBridgeContract.validate(
                request(
                    "colorScheme",
                    JSONObject().put("preference", "light").put("resolved", "light"),
                ),
            )
        assertEquals("light", explicit.payload?.getString("preference"))

        assertThrows(MarvoBridgeException::class.java) {
            MarvoBridgeContract.validate(
                request(
                    "colorScheme",
                    JSONObject().put("preference", "light").put("resolved", "dark"),
                ),
            )
        }
        assertThrows(MarvoBridgeException::class.java) {
            MarvoBridgeContract.validate(
                request(
                    "colorScheme",
                    JSONObject().put("preference", "system").put("resolved", "sepia"),
                ),
            )
        }
    }

    @Test
    fun shareRequiresTextOrFile() {
        assertThrows(MarvoBridgeException::class.java) {
            MarvoBridgeContract.validate(request("share", JSONObject()))
        }
    }

    private fun request(
        method: String,
        payload: JSONObject? = null,
    ): String =
        JSONObject()
            .put("id", "request-1")
            .put("method", method)
            .put("payload", payload ?: JSONObject.NULL)
            .toString()
}
