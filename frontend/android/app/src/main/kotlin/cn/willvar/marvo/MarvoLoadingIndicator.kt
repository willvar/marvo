package cn.willvar.marvo

import android.animation.ObjectAnimator
import android.content.Context
import android.graphics.Canvas
import android.graphics.Paint
import android.graphics.RectF
import android.view.View
import android.view.animation.LinearInterpolator
import androidx.core.content.ContextCompat

/** Native counterpart of frontend/src/styles/_loading.scss .page-loading-spinner. */
internal class MarvoLoadingIndicator(
    context: Context,
) : View(context) {
    private val strokeWidth = 2f * resources.displayMetrics.density
    private val bounds = RectF()
    private val track =
        Paint(Paint.ANTI_ALIAS_FLAG).apply {
            style = Paint.Style.STROKE
            this.strokeWidth = this@MarvoLoadingIndicator.strokeWidth
        }
    private val indicator =
        Paint(Paint.ANTI_ALIAS_FLAG).apply {
            style = Paint.Style.STROKE
            strokeCap = Paint.Cap.BUTT
            this.strokeWidth = this@MarvoLoadingIndicator.strokeWidth
        }
    private val rotationAnimator =
        ObjectAnimator.ofFloat(this, ROTATION, 0f, 360f).apply {
            duration = 800L
            interpolator = LinearInterpolator()
            repeatCount = ObjectAnimator.INFINITE
        }

    init {
        refreshColors()
    }

    fun refreshColors() {
        track.color = ContextCompat.getColor(context, R.color.marvo_loading_track)
        indicator.color = ContextCompat.getColor(context, R.color.marvo_loading_indicator)
        invalidate()
    }

    override fun onDraw(canvas: Canvas) {
        super.onDraw(canvas)
        val inset = strokeWidth / 2f
        bounds.set(inset, inset, width - inset, height - inset)
        canvas.drawOval(bounds, track)
        canvas.drawArc(bounds, -90f, 90f, false, indicator)
    }

    override fun onAttachedToWindow() {
        super.onAttachedToWindow()
        updateAnimation()
    }

    override fun onDetachedFromWindow() {
        rotationAnimator.cancel()
        super.onDetachedFromWindow()
    }

    override fun onVisibilityChanged(
        changedView: View,
        visibility: Int,
    ) {
        super.onVisibilityChanged(changedView, visibility)
        updateAnimation()
    }

    override fun onWindowVisibilityChanged(visibility: Int) {
        super.onWindowVisibilityChanged(visibility)
        updateAnimation()
    }

    private fun updateAnimation() {
        if (isAttachedToWindow && isShown) {
            if (!rotationAnimator.isStarted) rotationAnimator.start()
        } else {
            rotationAnimator.cancel()
            rotation = 0f
        }
    }
}
