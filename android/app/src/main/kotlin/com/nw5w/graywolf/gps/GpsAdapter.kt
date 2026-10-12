package com.nw5w.graywolf.gps

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import android.location.GnssStatus
import android.location.Location
import android.location.LocationListener
import android.location.LocationManager
import android.location.altitude.AltitudeConverter
import android.os.Build
import android.util.Log
import androidx.annotation.RequiresApi
import androidx.core.content.ContextCompat
import com.nw5w.graywolf.platformproto.GnssStatusUpdate
import com.nw5w.graywolf.platformproto.GpsFix
import com.nw5w.graywolf.platformproto.GpsSource
import com.nw5w.graywolf.platformproto.SatInfo
import com.nw5w.graywolf.platformsvc.PlatformServer
import java.util.concurrent.atomic.AtomicBoolean
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancelChildren
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.launch

/**
 * GPS producer: subscribes to the system LocationManager, translates
 * each Location into a GpsFix proto and each GnssStatus callback into
 * a GnssStatusUpdate proto, and pushes them through PlatformServer's
 * server-to-client broadcast.
 *
 * Lifecycle: start() in GraywolfService.onCreate after PlatformServer.start;
 * stop() in onDestroy before PlatformServer.stop. start() is a silent
 * no-op if ACCESS_FINE_LOCATION is not granted — the user must re-grant
 * via system settings, then re-launch the app.
 */
class GpsAdapter(
    private val ctx: Context,
    private val server: PlatformServer,
) {
    // lazy so unit tests can construct GpsAdapter with a mocked Context
    // without resolving the system service (toGpsFix doesn't touch it).
    private val locationManager: LocationManager by lazy {
        ctx.getSystemService(Context.LOCATION_SERVICE) as LocationManager
    }

    @Volatile private var lastSatCount: Int = 0
    @Volatile private var started: Boolean = false

    // Android Location.altitude is WGS84 ellipsoid height, not "meters above
    // sea level". It can differ from MSL by tens of metres depending on location.
    // Android 14+ provides the platform AltitudeConverter; run it off the main
    // thread because its first geoid-model load may take several seconds.
    // Serial execution also prevents an older fix from overtaking a newer one.
    @OptIn(ExperimentalCoroutinesApi::class)
    private val altitudeScope = CoroutineScope(
        SupervisorJob() + Dispatchers.IO.limitedParallelism(1)
    )
    private val altitudeFailureLogged = AtomicBoolean(false)

    private val locationListener = LocationListener { loc -> onLocation(loc) }

    private val gnssStatusCallback = object : GnssStatus.Callback() {
        override fun onSatelliteStatusChanged(status: GnssStatus) {
            var used = 0
            val builder = GnssStatusUpdate.newBuilder()
            for (i in 0 until status.satelliteCount) {
                val isUsed = status.usedInFix(i)
                if (isUsed) used++
                builder.addSats(SatInfo.newBuilder()
                    .setSvid(status.getSvid(i))
                    .setConstellation(constellationName(status.getConstellationType(i)))
                    .setCn0Dbhz(status.getCn0DbHz(i).toDouble())
                    .setUsedInFix(isUsed)
                    .setElevationDeg(status.getElevationDegrees(i).toDouble())
                    .setAzimuthDeg(status.getAzimuthDegrees(i).toDouble())
                    .build())
            }
            lastSatCount = used
            server.broadcastGnssStatus(builder
                .setSatsInView(status.satelliteCount)
                .setSatsUsed(used)
                .build())
        }
    }

    fun start() {
        if (started) return
        if (ContextCompat.checkSelfPermission(ctx, Manifest.permission.ACCESS_FINE_LOCATION)
            != PackageManager.PERMISSION_GRANTED) {
            Log.i(TAG, "GpsAdapter.start skipped — ACCESS_FINE_LOCATION not granted")
            return
        }
        // Devices without a GPS HAL (Facebook Portal, Android TV boxes, the
        // emulator) have no GPS_PROVIDER, and requestLocationUpdates throws
        // IllegalArgumentException("provider doesn't exist: gps"). Skip cleanly,
        // same posture as the missing-permission case above: the app runs
        // without GPS rather than crashing onCreate (issue #338).
        if (LocationManager.GPS_PROVIDER !in locationManager.allProviders) {
            Log.i(TAG, "GpsAdapter.start skipped — no GPS provider on this device")
            return
        }
        try {
            altitudeFailureLogged.set(false)
            locationManager.requestLocationUpdates(
                LocationManager.GPS_PROVIDER,
                10_000L, 0f, locationListener
            )
            locationManager.registerGnssStatusCallback(gnssStatusCallback, /* handler = */ null)
            started = true
            Log.i(TAG, "GpsAdapter started: GPS_PROVIDER 10s/0m + GNSS status callback")
        } catch (se: SecurityException) {
            Log.w(TAG, "GpsAdapter start hit SecurityException: $se")
            rollbackPartialStart()
        } catch (iae: IllegalArgumentException) {
            // Belt-and-suspenders for the no-provider race or a device where
            // registerGnssStatusCallback rejects the request after the guard.
            Log.w(TAG, "GpsAdapter start hit IllegalArgumentException: $iae")
            rollbackPartialStart()
        }
    }

    // requestLocationUpdates may have registered the listener before a later
    // call in start() threw; started stays false, so stop() (which only cleans
    // up when started) would leak it. Tear down whatever did register.
    private fun rollbackPartialStart() {
        try { locationManager.removeUpdates(locationListener) } catch (_: Throwable) {}
        try { locationManager.unregisterGnssStatusCallback(gnssStatusCallback) } catch (_: Throwable) {}
    }

    fun stop() {
        if (!started) return
        started = false
        try { locationManager.removeUpdates(locationListener) } catch (_: Throwable) {}
        try { locationManager.unregisterGnssStatusCallback(gnssStatusCallback) } catch (_: Throwable) {}
        altitudeScope.coroutineContext.cancelChildren()
    }

    /** Visible for testing. Prefers orthometric (MSL/NN) altitude when present. */
    internal fun toGpsFix(loc: Location, satCount: Int, includeAltitude: Boolean = true): GpsFix {
        val hasMslAlt = Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE &&
            loc.hasMslAltitude()
        val hasAlt = includeAltitude && (hasMslAlt || loc.hasAltitude())
        val altitudeM = when {
            !hasAlt -> 0.0
            hasMslAlt -> loc.mslAltitudeMeters
            loc.hasAltitude() -> loc.altitude
            else -> 0.0
        }
        return GpsFix.newBuilder()
            .setLat(loc.latitude)
            .setLon(loc.longitude)
            .setAltM(altitudeM)
            .setHasAlt(hasAlt)
            .setSpeedMps(if (loc.hasSpeed()) loc.speed.toDouble() else 0.0)
            .setHasSpeed(loc.hasSpeed())
            .setCourseDeg(if (loc.hasBearing()) loc.bearing.toDouble() else 0.0)
            .setHasCourse(loc.hasBearing())
            .setTimeUnixMs(loc.time)
            .setHdop(0.0) // Android doesn't expose HDOP — see proto comment on accuracy_m.
            .setNumSats(satCount.coerceAtLeast(0))
            .setSource(GpsSource.GPS_SOURCE_ANDROID_GPS)
            .setAccuracyM(if (loc.hasAccuracy()) loc.accuracy.toDouble() else 0.0)
            .build()
    }

    private fun onLocation(loc: Location) {
        val satCount = lastSatCount

        // Keep all Android 14+ fixes on the serial worker, including those
        // with provider-supplied MSL altitude, so none overtake a conversion.
        // Older Android releases have no built-in geoid converter, so keep the
        // raw ellipsoid altitude there rather than shipping a large private model.
        val canConvertMsl = Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE
        if (!canConvertMsl) {
            server.broadcastGpsFix(toGpsFix(loc, satCount))
            return
        }

        val converted = Location(loc)
        altitudeScope.launch {
            if (!converted.hasMslAltitude() && converted.hasAltitude()) {
                try {
                    Api34AltitudeConverter.addMslAltitude(ctx, converted)
                } catch (ce: CancellationException) {
                    throw ce
                } catch (e: Exception) {
                    ensureActive()
                    if (altitudeFailureLogged.compareAndSet(false, true)) {
                        Log.w(TAG, "MSL altitude conversion failed; omitting altitude for unconverted fixes", e)
                    }
                }
            }
            ensureActive()
            if (started) {
                // Android 14+ reports only MSL altitude. If conversion fails,
                // keep the position but omit altitude instead of mixing MSL
                // and ellipsoid heights within a session.
                server.broadcastGpsFix(toGpsFix(converted, satCount,
                    includeAltitude = converted.hasMslAltitude()))
            }
        }
    }

    private fun constellationName(type: Int): String = when (type) {
        GnssStatus.CONSTELLATION_GPS -> "GPS"
        GnssStatus.CONSTELLATION_GLONASS -> "GLONASS"
        GnssStatus.CONSTELLATION_BEIDOU -> "BEIDOU"
        GnssStatus.CONSTELLATION_GALILEO -> "GALILEO"
        GnssStatus.CONSTELLATION_QZSS -> "QZSS"
        GnssStatus.CONSTELLATION_SBAS -> "SBAS"
        else -> "UNKNOWN"
    }

    /**
     * Isolated so pre-Android-14 devices never initialize the API-34-only
     * AltitudeConverter class. One converter instance retains its local geoid
     * cache across fixes.
     */
    @RequiresApi(Build.VERSION_CODES.UPSIDE_DOWN_CAKE)
    private object Api34AltitudeConverter {
        private val converter by lazy { AltitudeConverter() }

        fun addMslAltitude(context: Context, location: Location) {
            converter.addMslAltitudeToLocation(context, location)
        }
    }

    companion object { private const val TAG = "GpsAdapter" }
}
