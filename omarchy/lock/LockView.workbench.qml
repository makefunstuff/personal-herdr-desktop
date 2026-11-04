import QtQuick
import QtQuick.Window
import Quickshell

// Workbench lock screen: an engineering-console module over the theme wallpaper (amber frame, node
// header, big clock, status LEDs). Same public API as the stock omarchy.lock LockView (LockView.qml.orig),
// so Service.qml is untouched. The theme-set hook copies this to LockView.qml for the workbench theme.
Item {
  id: root

  property string backgroundPath: ""
  property int backgroundVersion: 0
  property bool fingerprintConfigured: false
  property bool authenticatingPassword: false
  property string failureMessage: ""
  property int failedAttempts: 0
  property bool inputEnabled: true
  property bool loadBackground: true
  property string passwordText: ""
  property bool syncingPasswordText: false

  readonly property real dpr: (Window.window && Window.window.devicePixelRatio > 0) ? Window.window.devicePixelRatio : (Screen.devicePixelRatio > 0 ? Screen.devicePixelRatio : 1)
  readonly property real u: 1.25 / dpr * 1.6
  readonly property string mono: "JetBrainsMono Nerd Font"
  readonly property string userName: Quickshell.env("USER") || Quickshell.env("LOGNAME") || ""
  readonly property string host: Quickshell.env("HOSTNAME") || "workbench"

  readonly property color bg: "#0d0f10"
  readonly property color panel: "#1b1f21"
  readonly property color raised: "#22282b"
  readonly property color line: "#4e5b57"
  readonly property color fg: "#dfe4e1"
  readonly property color dim: "#66727a"
  readonly property color amber: "#ffb000"
  readonly property color green: "#5fd38a"
  readonly property color red: "#ff5a4a"

  property date now: new Date()

  signal submitPassword(string password)
  signal passwordTextEdited(string password)
  signal clearFailureRequested()
  signal wakeRequested()

  function px(n) { return Math.max(1, Math.round(n * u)) }
  function pad2(n) { return (n < 10 ? "0" : "") + n }
  function fileUrl(path) {
    if (!path) return ""
    var encoded = String(path).split("/").map(encodeURIComponent).join("/")
    return "file://" + encoded + "?v=" + backgroundVersion
  }
  function forcePasswordFocus() { passwordInput.forceActiveFocus() }
  function syncPasswordText() {
    if (passwordInput.text === passwordText) return
    syncingPasswordText = true
    passwordInput.text = passwordText
    syncingPasswordText = false
  }
  function submitNow() {
    var submitted = root.passwordText
    root.passwordTextEdited("")
    if (submitted.length > 0) root.submitPassword(submitted)
    root.forcePasswordFocus()
  }

  onPasswordTextChanged: syncPasswordText()
  onInputEnabledChanged: { if (inputEnabled) Qt.callLater(forcePasswordFocus) }
  Component.onCompleted: { console.log("workbench lock view ready"); syncPasswordText(); if (inputEnabled) Qt.callLater(forcePasswordFocus) }

  Timer { interval: 1000; repeat: true; running: true; onTriggered: root.now = new Date() }

  component Mono: Text {
    textFormat: Text.PlainText
    color: root.fg
    font.family: root.mono
    font.pixelSize: root.px(13)
    renderType: Text.NativeRendering
  }

  component Led: Rectangle {
    property color on: root.green
    property bool lit: true
    width: root.px(8); height: root.px(8)
    color: lit ? on : root.raised
    border.color: root.bg; border.width: root.px(1)
  }

  Rectangle {
    anchors.fill: parent
    color: root.bg

    Image {
      anchors.fill: parent
      source: root.loadBackground ? root.fileUrl(root.backgroundPath) : ""
      fillMode: Image.PreserveAspectCrop
      asynchronous: true
      cache: false
      sourceSize.width: width
      sourceSize.height: height
    }
    Rectangle { anchors.fill: parent; color: "#000000"; opacity: 0.45 }

    MouseArea {
      anchors.fill: parent
      hoverEnabled: true
      onClicked: { root.wakeRequested(); root.forcePasswordFocus() }
      onPositionChanged: root.wakeRequested()
    }

    // Clock, top centre, as a segment-style readout
    Column {
      anchors.horizontalCenter: parent.horizontalCenter
      y: parent.height * 0.14
      spacing: root.px(4)
      Mono {
        anchors.horizontalCenter: parent.horizontalCenter
        text: root.pad2(root.now.getHours()) + ":" + root.pad2(root.now.getMinutes())
        font.pixelSize: root.px(96); font.bold: true
        color: root.amber
      }
      Mono {
        anchors.horizontalCenter: parent.horizontalCenter
        text: Qt.formatDate(root.now, "dddd d MMMM yyyy").toUpperCase()
        font.pixelSize: root.px(14); font.letterSpacing: root.px(3)
        color: root.dim
      }
    }

    // The access module
    Rectangle {
      id: module
      width: root.px(460)
      height: body.height + head.height + root.px(4)
      anchors.centerIn: parent
      anchors.verticalCenterOffset: root.px(60)
      color: root.panel
      border.color: root.amber; border.width: root.px(2)

      Rectangle { // drop shadow
        z: -1; x: root.px(10); y: root.px(10); width: parent.width; height: parent.height; color: "#000000"; opacity: 0.6
      }

      Row {
        id: head
        x: root.px(2); y: root.px(2)
        width: parent.width - root.px(4); height: root.px(32)
        Rectangle {
          width: headLabel.width + root.px(24); height: parent.height; color: root.amber
          Mono { id: headLabel; anchors.centerIn: parent; text: "ACCESS NODE"; font.bold: true; color: root.bg }
        }
        Rectangle {
          width: parent.width - x - nodeId.width - root.px(16); height: parent.height; color: root.raised
          Mono { anchors.verticalCenter: parent.verticalCenter; x: root.px(12); text: "LOCKED"; color: root.red; font.bold: true }
        }
        Rectangle {
          width: nodeId.width + root.px(16); height: parent.height; color: root.raised
          Rectangle { width: root.px(2); height: parent.height; color: root.line }
          Mono { id: nodeId; anchors.centerIn: parent; text: root.host.toUpperCase(); color: root.dim }
        }
      }
      Rectangle { x: root.px(2); y: head.y + head.height; width: parent.width - root.px(4); height: root.px(2); color: root.line }

      Column {
        id: body
        x: root.px(22); y: head.y + head.height + root.px(20)
        width: parent.width - root.px(44)
        spacing: root.px(10)

        Row {
          spacing: root.px(10)
          Mono { width: root.px(70); text: "USER"; color: root.dim; font.bold: true }
          Mono { text: root.userName.toUpperCase(); font.bold: true }
        }

        Row {
          spacing: root.px(10)
          Mono { width: root.px(70); anchors.verticalCenter: parent.verticalCenter; text: "KEY"; color: root.dim; font.bold: true }
          Rectangle {
            width: body.width - root.px(80); height: root.px(34)
            color: "#090a0b"
            border.color: passwordInput.activeFocus ? root.amber : root.line; border.width: root.px(2)
            clip: true

            Mono {
              anchors.verticalCenter: parent.verticalCenter; x: root.px(10)
              visible: passwordInput.text.length === 0 && !root.authenticatingPassword
              text: "enter password ▸"; color: root.dim
            }
            TextInput {
              id: passwordInput
              anchors.fill: parent
              anchors.leftMargin: root.px(10); anchors.rightMargin: root.px(10)
              verticalAlignment: TextInput.AlignVCenter
              activeFocusOnPress: true
              clip: true
              enabled: root.inputEnabled && !root.authenticatingPassword
              readOnly: root.authenticatingPassword
              echoMode: TextInput.Password
              passwordCharacter: "■"
              passwordMaskDelay: 0
              color: root.amber
              selectionColor: root.amber
              selectedTextColor: root.bg
              font.family: root.mono
              font.pixelSize: root.px(15)
              font.letterSpacing: root.px(2)
              renderType: Text.NativeRendering
              cursorVisible: activeFocus && root.inputEnabled && !root.authenticatingPassword
              cursorDelegate: Rectangle {
                width: root.px(9)
                color: root.amber
                visible: passwordInput.cursorVisible && blink.on
                Timer { id: blink; property bool on: true; interval: 530; repeat: true; running: passwordInput.activeFocus; onTriggered: on = !on }
              }
              onTextChanged: {
                if (!root.syncingPasswordText) root.passwordTextEdited(text)
                if (text.length > 0) root.wakeRequested()
                if (text.length > 0 && root.failureMessage.length > 0) root.clearFailureRequested()
              }
              onAccepted: root.submitNow()
              Keys.onPressed: function(event) {
                root.wakeRequested()
                if (event.key === Qt.Key_Escape || (event.modifiers & Qt.ControlModifier && event.key === Qt.Key_U)) {
                  root.passwordTextEdited("")
                  event.accepted = true
                }
              }
            }
          }
        }

        Rectangle { width: body.width; height: root.px(2); color: root.line }

        Row {
          spacing: root.px(12)
          height: root.px(26)
          Row {
            anchors.verticalCenter: parent.verticalCenter
            spacing: root.px(4)
            Led { on: root.green; lit: !root.authenticatingPassword && root.failureMessage.length === 0 }
            Led { on: root.amber; lit: root.authenticatingPassword }
            Led { on: root.red; lit: root.failureMessage.length > 0 && !root.authenticatingPassword }
          }
          Mono {
            anchors.verticalCenter: parent.verticalCenter
            width: body.width - root.px(60)
            elide: Text.ElideRight
            font.bold: true
            text: root.authenticatingPassword
              ? "VERIFYING…"
              : (root.failureMessage.length > 0
                ? "REJECTED · ATTEMPT " + root.failedAttempts
                : (root.fingerprintConfigured ? "READY · OR TOUCH THE SENSOR" : "READY · ENTER TO UNLOCK"))
            color: root.failureMessage.length > 0 && !root.authenticatingPassword ? root.red
              : (root.authenticatingPassword ? root.amber : root.dim)
          }
        }
        Item { width: 1; height: root.px(6) }
      }
    }

    Mono {
      anchors.horizontalCenter: parent.horizontalCenter
      anchors.bottom: parent.bottom; anchors.bottomMargin: root.px(28)
      text: "HERDR WORKBENCH  //  OMARCHY"
      font.pixelSize: root.px(11); font.letterSpacing: root.px(3)
      color: root.dim
    }
  }
}
