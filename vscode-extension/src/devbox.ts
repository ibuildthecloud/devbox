import { window, workspace, commands, ProgressLocation, Uri, ConfigurationTarget } from 'vscode';
import { spawn, spawnSync } from 'node:child_process';


interface Message {
    status: string
}

export async function devboxReopen() {
    await window.withProgress({
        location: ProgressLocation.Notification,
        title: "Setting up your Devbox environment. Please don't close vscode.",
        cancellable: true
    },
        async (progress, token) => {
            token.onCancellationRequested(() => {
                console.log("User canceled the long running operation");
            });

            const p = new Promise<void>(async (resolve, reject) => {

                if (workspace.workspaceFolders) {
                    const workingDir = workspace.workspaceFolders[0].uri;
                    const dotdevbox = Uri.joinPath(workingDir, '/.devbox');
                    progress.report({ message: 'Installing devbox packages...', increment: 25 });
                    await setupDotDevbox(workingDir, dotdevbox);

                    // setup required vscode settings
                    progress.report({ message: 'Updating configurations...', increment: 50 });
                    updateVSCodeConf();

                    // Calling CLI to compute devbox env
                    progress.report({ message: 'Calling Devbox to setup environment...', increment: 80 });
                    // To use a custom compiled devbox when testing, change this to an absolute path.
                    const devbox = 'devbox';
                    // run devbox integrate and then close this window
                    let child = spawn(devbox, ['integrate', 'vscode'], {
                        cwd: workingDir.path,
                        stdio: [0, 1, 2, 'ipc']
                    });
                    // if CLI closes before sending "finished" message
                    child.on('close', (code: number) => {
                        console.log("child process closed with exit code:", code);
                        window.showErrorMessage("Failed to setup devbox environment.");
                        reject();
                    });
                    // send config path to CLI
                    child.send({ configDir: workingDir.path });
                    // handle CLI finishing the env and sending  "finished"
                    child.on('message', function (msg: Message, handle) {
                        if (msg.status === "finished") {
                            progress.report({ message: 'Finished setting up! Reloading the window...', increment: 100 });
                            resolve();
                            commands.executeCommand("workbench.action.closeWindow");
                        }
                        else {
                            console.log(msg);
                            window.showErrorMessage("Failed to setup devbox environment.");
                            reject();
                        }
                    });
                }
            });
            return p;
        }
    );
}

async function setupDotDevbox(workingDir: Uri, dotdevbox: Uri) {
    try {
        // check if .devbox exists
        await workspace.fs.stat(dotdevbox);
    } catch (error) {
        //.devbox doesn't exist
        // running devbox shellenv to create it
        spawnSync('devbox', ['shellenv'], {
            cwd: workingDir.path
        });
    }
}

function updateVSCodeConf() {
    if (process.platform === 'darwin') {
        const shell = process.env["SHELL"] ?? "/bin/zsh";
        const shellArgsMap = (shellType: string) => {
            switch (shellType) {
                case "fish":
                    // We special case fish here because fish's `fish_add_path` function
                    // tends to prepend to PATH by default, hence sourcing the fish config after
                    // vscode reopens in devbox environment, overwrites devbox packages and 
                    // might cause confusion for users as to why their system installed packages
                    // show up when they type for example `which go` as opposed to the packages
                    // installed by devbox.
                    return ["--no-config"];
                default:
                    return [];
            }
        };
        const shellTypeSlices = shell.split("/");
        const shellType = shellTypeSlices[shellTypeSlices.length - 1];
        shellArgsMap(shellType);
        const devboxCompatibleShell = {
            "devboxCompatibleShell": {
                "path": shell,
                "args": shellArgsMap(shellType)
            }
        };

        workspace.getConfiguration().update(
            'terminal.integrated.profiles.osx',
            devboxCompatibleShell,
            ConfigurationTarget.Workspace
        );
        workspace.getConfiguration().update(
            'terminal.integrated.defaultProfile.osx',
            'devboxCompatibleShell',
            ConfigurationTarget.Workspace);
    }
}