import org.jetbrains.kotlin.gradle.dsl.JvmTarget
import java.net.URI
import java.util.Properties

plugins {
    id("com.android.application")
    id("io.gitlab.arturbosch.detekt")
    id("org.jlleitschuh.gradle.ktlint")
    kotlin("android")
}

val versionProperties =
    Properties().apply {
        rootProject.file("version.properties").inputStream().use(::load)
    }
val marvoVersionCode =
    versionProperties.getProperty("VERSION_CODE")?.toIntOrNull()
        ?: error("VERSION_CODE must be a positive integer")
val marvoVersionName = versionProperties.getProperty("VERSION_NAME")?.trim().orEmpty()
val versionNamePattern = Regex("[0-9]+(?:\\.[0-9]+){0,2}(?:[-+][0-9A-Za-z.-]+)?")
require(marvoVersionCode > 0) { "VERSION_CODE must be greater than zero" }
require(versionNamePattern.matches(marvoVersionName)) { "VERSION_NAME is invalid" }

val configuredServerOrigin =
    providers
        .gradleProperty("marvo.serverOrigin")
        .orNull
        ?.trim()
        ?.trimEnd('/')
val serverOrigin = configuredServerOrigin ?: "https://marvo.invalid"
val parsedOrigin = URI(serverOrigin)

fun isPrivateDebugHost(host: String?): Boolean {
    val normalized = host?.lowercase()?.trim('[', ']') ?: return false
    if (normalized == "localhost" || normalized == "::1") return true
    val parts = normalized.split('.')
    if (parts.size != 4) return false
    val octets = parts.map { it.toIntOrNull() ?: return false }
    if (octets.any { it !in 0..255 }) return false
    return octets[0] == 10 ||
        octets[0] == 127 ||
        (octets[0] == 192 && octets[1] == 168) ||
        (octets[0] == 172 && octets[1] in 16..31)
}

val isOriginShapeValid =
    parsedOrigin.host != null &&
        parsedOrigin.userInfo == null &&
        parsedOrigin.path.orEmpty().isEmpty() &&
        parsedOrigin.rawQuery == null &&
        parsedOrigin.rawFragment == null
val isPrivateDebugHTTP = parsedOrigin.scheme == "http" && isPrivateDebugHost(parsedOrigin.host)
require(
    isOriginShapeValid && (parsedOrigin.scheme == "https" || isPrivateDebugHTTP),
) {
    "marvo.serverOrigin must be an HTTPS origin, or a private/loopback HTTP origin for debug builds"
}

fun quoted(value: String): String = "\"${value.replace("\\", "\\\\").replace("\"", "\\\"")}\""

val signingPropertiesFile =
    providers.gradleProperty("marvo.signingFile").orNull?.let(rootProject::file)
        ?: rootProject.file("signing.properties")
val signingProperties =
    Properties().apply {
        if (signingPropertiesFile.isFile) signingPropertiesFile.inputStream().use(::load)
    }

fun signingSecret(name: String): String? {
    signingProperties.getProperty(name)?.takeIf(String::isNotBlank)?.let { return it }
    val secretFile = signingProperties.getProperty("${name}File")?.takeIf(String::isNotBlank) ?: return null
    return rootProject
        .file(secretFile)
        .takeIf { it.isFile }
        ?.readText()
        ?.trim()
        ?.takeIf(String::isNotBlank)
}
val signingStoreFile = signingProperties.getProperty("storeFile")?.takeIf(String::isNotBlank)
val signingStorePassword = signingSecret("storePassword")
val signingKeyAlias = signingProperties.getProperty("keyAlias")?.takeIf(String::isNotBlank)
val signingKeyPassword = signingSecret("keyPassword")
val hasReleaseSigning =
    signingPropertiesFile.isFile &&
        signingStoreFile != null &&
        signingStorePassword != null &&
        signingKeyAlias != null &&
        signingKeyPassword != null

android {
    namespace = "cn.willvar.marvo"
    compileSdk = 36

    defaultConfig {
        applicationId = "cn.willvar.marvo"
        minSdk = 26
        targetSdk = 36
        versionCode = marvoVersionCode
        versionName = marvoVersionName

        buildConfigField("String", "SERVER_ORIGIN", quoted(serverOrigin))
    }

    signingConfigs {
        if (hasReleaseSigning) {
            create("release") {
                storeFile = file(signingStoreFile!!)
                storePassword = signingStorePassword
                keyAlias = signingKeyAlias
                keyPassword = signingKeyPassword
            }
        }
    }

    buildTypes {
        debug {
            applicationIdSuffix = ".debug"
            versionNameSuffix = "-debug"
        }
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            signingConfig = signingConfigs.findByName("release")
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
        }
    }

    buildFeatures {
        buildConfig = true
        viewBinding = true
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    packaging {
        resources.excludes += "/META-INF/{AL2.0,LGPL2.1}"
    }
}

kotlin {
    compilerOptions {
        jvmTarget.set(JvmTarget.JVM_17)
        allWarningsAsErrors.set(true)
        extraWarnings.set(true)
    }
}

detekt {
    buildUponDefaultConfig = true
    config.setFrom(rootProject.files("config/detekt/detekt.yml"))
    parallel = true
    ignoreFailures = false
}

ktlint {
    version.set("1.8.0")
    android.set(true)
    outputToConsole.set(true)
    ignoreFailures.set(false)
}

val frontendDist = rootProject.projectDir.parentFile.resolve("dist")

fun registerWebAssetsTask(
    name: String,
    applicationID: String,
) = tasks.register<Sync>(name) {
    group = "marvo"
    description = "Copies the current Vite build and Android release metadata into the APK."
    from(frontendDist)
    val output = layout.buildDirectory.dir("generated/$name")
    into(output)
    doFirst {
        require(frontendDist.resolve("index.html").isFile) {
            "frontend/dist is missing; run npm --prefix frontend run build first"
        }
    }
    doLast {
        output.get().file("marvo-app.json").asFile.writeText(
            """
            {
              "application_id": "$applicationID",
              "version_code": $marvoVersionCode,
              "version_name": "$marvoVersionName"
            }
            """.trimIndent() + "\n",
        )
    }
}

val validateServerOrigin by tasks.registering {
    doLast {
        require(configuredServerOrigin != null) {
            "Pass the deployment origin with -Pmarvo.serverOrigin=https://your-domain.example"
        }
    }
}
val validateReleaseServerOrigin by tasks.registering {
    doLast {
        require(parsedOrigin.scheme == "https") {
            "Release builds require an HTTPS marvo.serverOrigin"
        }
    }
}
val validateReleaseSigning by tasks.registering {
    doLast {
        require(hasReleaseSigning) {
            "Copy signing.example.properties to signing.properties and provide all release signing values"
        }
        require(file(signingStoreFile!!).isFile) {
            "The release signing keystore does not exist"
        }
    }
}
val prepareDebugWebAssets = registerWebAssetsTask("prepareDebugWebAssets", "cn.willvar.marvo.debug")
val prepareReleaseWebAssets = registerWebAssetsTask("prepareReleaseWebAssets", "cn.willvar.marvo")
android.sourceSets
    .getByName("debug")
    .assets
    .srcDir(layout.buildDirectory.dir("generated/prepareDebugWebAssets"))
android.sourceSets
    .getByName("release")
    .assets
    .srcDir(layout.buildDirectory.dir("generated/prepareReleaseWebAssets"))
tasks.configureEach {
    when (name) {
        "preDebugBuild" -> {
            dependsOn(prepareDebugWebAssets, validateServerOrigin)
        }

        "preReleaseBuild" -> {
            dependsOn(
                prepareReleaseWebAssets,
                validateServerOrigin,
                validateReleaseServerOrigin,
                validateReleaseSigning,
            )
        }
    }
}

dependencies {
    implementation("androidx.activity:activity-ktx:1.13.0")
    implementation("androidx.appcompat:appcompat:1.7.1")
    // 1.19 requires compileSdk 37 and AGP 9.1; this project intentionally remains on the API 36 toolchain.
    //noinspection GradleDependency
    implementation("androidx.core:core-ktx:1.18.0")
    implementation("androidx.webkit:webkit:1.17.0")
    implementation("com.google.android.material:material:1.14.0")
    implementation("com.journeyapps:zxing-android-embedded:4.3.0")
    implementation("com.squareup.okhttp3:okhttp:5.4.0")
    testImplementation("junit:junit:4.13.2")
    testImplementation("org.json:json:20250517")
}
