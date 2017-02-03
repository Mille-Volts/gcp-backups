package main

import (
  "os/exec"
  "log"
  "flag"
  "encoding/json"
  "strings"
  "errors"
  "time"
  "fmt"
)

type Disk struct {
  Name      string
  Id        string
  Zone      string
  Snapshots []Snapshot
}

type Snapshot struct {
  Name string
  Id   string
}

func Max(x, y int) int {
  if x > y {
    return x
  }
  return y
}

func getCommandResult(command string, args []string) ([]byte, error) {
  cmd := exec.Command(command, args...)
  cmdOut, cmdErr := cmd.CombinedOutput()
  if cmdErr != nil {
    log.Printf("%s", cmdOut)
    log.Fatal(cmdErr)
    return make([]byte, 0), errors.New("Command error: `" + strings.Join(args, " ") + "`")
  }

  return cmdOut, nil
}

func getDisksToSnapshot(filter string) ([]Disk, error) {
  disks := make([]Disk, 0)

  cmdListDisksOut, err := getCommandResult("gcloud", []string{"beta", "compute", "disks", "list", "--filter", filter, "--format", "json"})
  if err != nil {
    return disks, err
  }
  json.Unmarshal(cmdListDisksOut, &disks)

  return disks, nil
}

func getDiskSnapshots(disk Disk) ([]Snapshot, error) {
  snapshots := make([]Snapshot, 0)

  cmdSnapshotsOut, err := getCommandResult("gcloud", []string{"beta", "compute", "snapshots", "list", "--sort-by", "~creationTimestamp", "--filter", "sourceDiskId = " + disk.Id, "--format", "json"})
  if err != nil {
    return snapshots, err
  }
  json.Unmarshal(cmdSnapshotsOut, &snapshots)

  return snapshots, nil
}

func createSnapshotForDisk(disk Disk, dryRun bool) (Snapshot, error) {
  // Asynchronous
  now := time.Now()
  timePart := fmt.Sprintf("%04d%02d%02d%02d%02d", now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute())

  maxSnapshotName := 55
  spaceLeft := maxSnapshotName - len(timePart) - len("" + disk.Id)

  namesParts := strings.Split(disk.Name, "-")
  namesPartsLen := len(namesParts)
  startPartEnd := 0
  endPartStart := namesPartsLen

  for namePartIndex := 0; namePartIndex < namesPartsLen; namePartIndex++ {
    startPartLen := len(namesParts[namePartIndex]);
    endPartLen := len(namesParts[namesPartsLen - namePartIndex - 1]);
    if spaceLeft > startPartLen {
      startPartEnd++
      spaceLeft -= startPartLen + 1
    }
    if spaceLeft > endPartLen {
      endPartStart--
      spaceLeft -= endPartLen + 1
    }
  }

  if startPartEnd >= endPartStart {
    endPartStart = namesPartsLen
  }

  name := strings.Join(namesParts[0:startPartEnd], "-") + "-" + strings.Join(namesParts[endPartStart:], "-") + "-" + disk.Id + "-" + timePart
  name = strings.Replace(name, "--", "-", -1)

  snapshot := Snapshot{Name: name}

  if dryRun {
    return snapshot, nil
  }

  _, err := getCommandResult("gcloud", []string{"beta", "compute", "disks", "snapshot", disk.Name, "--zone", disk.Zone, "--snapshot-names", name})

  return snapshot, err
}

func deleteSnapshot(snapshot Snapshot, dryRun bool) error {
  if dryRun {
    return nil
  }

  _, err := getCommandResult("gcloud", []string{"beta", "compute", "snapshots", "delete", snapshot.Name})

  return err
}

func main() {
  var filter string
  flag.StringVar(&filter, "filter", "labels.env = production", "Filter to use for disks to snapshot")
  var limit int
  flag.IntVar(&limit, "limit", 7, "Number of snapshots to keep")
  var dryRun bool
  flag.BoolVar(&dryRun, "dry-run", false, "Don't really do backups and deletions but show logs")

  flag.Parse()

  log.Printf("Backup of GCP disks using filter '%s'\n", filter)

  if dryRun {
    log.Println("")
    log.Println("DRY RUN MODE: nothing is created or deleted", filter)
  }

  log.Println("")

  disks, disksErr := getDisksToSnapshot(filter)

  if disksErr != nil {
    log.Fatal(disksErr)
    return
  }

  if len(disks) == 0 {
    log.Println("No disk to snapshot")
    return
  }
  log.Println("Disks and snapshots found:")
  for diskIndex := 0; diskIndex < len(disks); diskIndex++ {
    disk := &disks[diskIndex]
    log.Printf("%02d ) %s\n", diskIndex + 1, disk.Name)
    snapshots, snapshotsErr := getDiskSnapshots(*disk)
    if snapshotsErr != nil {
      log.Fatal(disksErr)
      return
    }
    disk.Snapshots = snapshots
    for snapshotIndex := 0; snapshotIndex < len(snapshots); snapshotIndex++ {
      snapshot := snapshots[snapshotIndex]
      log.Printf("      - %s\n", snapshot.Name)
    }
  }
  log.Println("")

  time.Sleep(time.Duration(2) * time.Second)

  log.Println("Creating snapshots...")

  snapshotsCreated := make(chan Snapshot, len(disks))
  for diskIndex := 0; diskIndex < len(disks); diskIndex++ {
    go func(disk Disk) {
      log.Printf("Creating snapshot for disk %s\n", disk.Name)
      snapshot, snapshotErr := createSnapshotForDisk(disk, dryRun)
      if snapshotErr != nil {
        log.Fatal(snapshotErr)
        return
      }
      snapshotsCreated <- snapshot
    }(disks[diskIndex])
  }
  for diskIndex := 0; diskIndex < len(disks); diskIndex++ {
    diskBackuped := &disks[diskIndex]
    snapshotCreated := <-snapshotsCreated
    newSnapshots := make([]Snapshot, len(diskBackuped.Snapshots) + 1)
    copy(newSnapshots[1:], diskBackuped.Snapshots)
    newSnapshots[0] = snapshotCreated
    diskBackuped.Snapshots = newSnapshots
    log.Printf("Created snapshot %s\n", snapshotCreated.Name)
  }
  log.Printf("Created %d snapshots", len(disks))
  log.Println("")

  time.Sleep(time.Duration(2) * time.Second)

  log.Printf("Deleting old snapshots (limit: %d)\n", limit)

  oldSnapshotsDeleted := make(chan Disk, len(disks))
  for diskIndex := 0; diskIndex < len(disks); diskIndex++ {
    diskToClean := &disks[diskIndex]
    diff := len(diskToClean.Snapshots) - limit
    if diff <= 0 {
      oldSnapshotsDeleted <- *diskToClean
      continue
    }
    go func(disk Disk) {
      diff := len(disk.Snapshots) - limit
      snapshotsDeletedForDisk := make(chan Snapshot, diff)
      log.Printf("Deleting %d old snapshot(s) for disk %s\n", diff, disk.Name)
      for snapshotIndex := limit; snapshotIndex < len(disk.Snapshots); snapshotIndex++ {
        go func(snapshotToDelete Snapshot) {
          snapshotDeleteErr := deleteSnapshot(snapshotToDelete, dryRun)
          if snapshotDeleteErr != nil {
            log.Fatal(snapshotDeleteErr)
            return
          }
          snapshotsDeletedForDisk <- snapshotToDelete
        }(disk.Snapshots[snapshotIndex])
      }
      for snapshotIndex := limit; snapshotIndex < len(disk.Snapshots); snapshotIndex++ {
        snapshotDeleted := <-snapshotsDeletedForDisk
        log.Printf("Deleted snapshot %s\n", snapshotDeleted.Name)
      }
      oldSnapshotsDeleted <- disk
    }(*diskToClean)
  }
  for diskIndex := 0; diskIndex < len(disks); diskIndex++ {
    diskCleaned := <-oldSnapshotsDeleted
    log.Printf("Cleaned disk %s: %d snapshot(s) deleted\n", diskCleaned.Name, Max(0, len(diskCleaned.Snapshots) - limit))
  }
  log.Println("")
  log.Printf("Backup complete!")

  if dryRun {
    log.Println("")
    log.Println("DRY RUN MODE: nothing has been created or deleted", filter)
  }
}
