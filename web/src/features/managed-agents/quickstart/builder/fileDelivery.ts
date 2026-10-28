const fileDeliveryContract = `# Platform file delivery contract

These platform rules take precedence over conflicting file delivery instructions above. When the user requests a deliverable, write it to /mnt/user-data/outputs/<relative-path> (also mounted at /mnt/session/outputs). Only this mount publishes files as downloadable session resources; a path in /home, /tmp or the working directory is not a delivered file. Verify the written file before reporting delivery, and link it using [filename](/outputs/<relative-path>). If publishing fails, report the delivery failure separately from successful calculation. Ordinary source edits stay in the working directory unless the user asks for an exported copy. This contract does not change the requested response format when no file is requested.`;

export function builderFileDeliverySystem(system: string) {
  return system.endsWith(fileDeliveryContract) ? system : `${system}\n\n${fileDeliveryContract}`;
}
