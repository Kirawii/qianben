import java.io.File
plugins { id("com.android.application"); id("org.jetbrains.kotlin.android"); id("org.jetbrains.kotlin.kapt") }
android {
 namespace = "com.qianben.app"
 compileSdk = 35
 defaultConfig { applicationId = "com.qianben.app"; minSdk = 26; targetSdk = 35; versionCode = 13; versionName = "0.2.0-dev";testInstrumentationRunner="androidx.test.runner.AndroidJUnitRunner" }
 buildFeatures { buildConfig = true }
 compileOptions { sourceCompatibility = JavaVersion.VERSION_17; targetCompatibility = JavaVersion.VERSION_17 }
 kotlinOptions { jvmTarget = "17" }
}
dependencies {
 implementation("androidx.room:room-runtime:2.7.2")
 kapt("androidx.room:room-compiler:2.7.2")
 implementation("androidx.work:work-runtime:2.10.1")
 androidTestImplementation("androidx.test:runner:1.6.2")
 androidTestImplementation("androidx.test.ext:junit:1.2.1")
}

val deviceAssets = layout.buildDirectory.dir("generated/device-assets")
android.sourceSets.getByName("main").assets.srcDir(deviceAssets)
val generateDeviceEngine by tasks.registering {
 inputs.files(rootProject.fileTree("../backend/internal/ondevice"), rootProject.fileTree("../backend/internal/accounting"), rootProject.fileTree("../backend/internal/domain"), rootProject.fileTree("../backend/internal/evidence"), rootProject.fileTree("../backend/cmd/ondevice"), rootProject.file("../backend/go.mod"), rootProject.file("../backend/go.sum"), rootProject.file("../.tools/go/VERSION"))
 outputs.dir(deviceAssets)
 doLast {
  val repo = rootProject.projectDir.parentFile
  val portable = File(repo, ".tools/go/bin/go.exe")
  val go = if (portable.exists()) portable.absolutePath else "go"
  val destination = deviceAssets.get().asFile.resolve("ondevice").apply { mkdirs() }
  val process = ProcessBuilder(go, "-C", File(repo,"backend").absolutePath, "build", "-o", File(destination,"engine.wasm").absolutePath, "./cmd/ondevice").redirectErrorStream(true)
  process.environment().putAll(mapOf("GOOS" to "js", "GOARCH" to "wasm", "GOCACHE" to File(repo,"backend/.cache/go-build").absolutePath,"GOMODCACHE" to File(repo,"backend/.cache/go-mod").absolutePath))
  val child=process.start(); val output=child.inputStream.bufferedReader().readText(); check(child.waitFor()==0) { output }
  val rootProcess=ProcessBuilder(go,"env","GOROOT").start(); val goRoot=rootProcess.inputStream.bufferedReader().readText().trim(); check(rootProcess.waitFor()==0)
  File(goRoot,"lib/wasm/wasm_exec.js").copyTo(File(destination,"wasm_exec.js"),overwrite=true)
  File(goRoot,"LICENSE").copyTo(File(destination,"GO_LICENSE"),overwrite=true)
 }
}
tasks.named("preBuild") { dependsOn(generateDeviceEngine) }
