use std::path::{Path, PathBuf};

use common::bitvec::{BitSlice, BitVec};
use common::mmap::AdviceSetting;
use common::stored_bitslice::StoredBitSlice;
use common::types::PointOffsetType;
use common::universal_io::{
    CachedReadFs, OpenOptions, Populate, TypedStorage, UniversalRead, UniversalReadFs,
};

use super::dynamic_stored_flags::{DynamicFlagsStatus, FLAGS_FILE, status_file};
use crate::common::operation_error::{OperationError, OperationResult};

/// In-memory counterpart of `BitvecFlags`: persisted flags materialized into an
/// owned `BitVec`, no write path.
///
/// Vector-storage `deleted` sets take whole-point deletions from the live-reload
/// delta (`deleted_points`) and, for appended offsets, also fold per-vector
/// deletion bits that exist only in the on-disk flags file. Unlike
/// [`ReadOnlyRoaringFlags`](super::read_only_roaring_flags::ReadOnlyRoaringFlags)
/// this type does not keep a flags handle: `open` / `live_reload` open a fresh
/// one so caching backends see in-place bit writes. The owned bitvec is never
/// replaced wholesale, so in-memory deletions that are not yet flushed survive.
#[derive(Debug)]
#[allow(dead_code)] // pending: read-only vector storages will hold `deleted` as this
pub struct InMemoryBitvecFlags {
    /// Flags, materialized on open and patched in place on live-reload.
    bitvec: BitVec,
    /// Set-flag count, kept in sync with `bitvec`.
    count: usize,
    /// Directory of the dynamic flags files, if this set was opened from disk.
    /// `None` for flags materialized from another format (e.g. immutable dense
    /// `deleted.dat`).
    directory: Option<PathBuf>,
}

/// Read-only mmap options: never writable, lazily paged, nothing populated.
fn bitslice_open_options(populate: Populate) -> OpenOptions {
    OpenOptions {
        writeable: false,
        need_sequential: false,
        populate,
        advice: AdviceSetting::Global,
    }
}

/// Logical flag count from the status file. The flags file is padded past this.
fn read_logical_len<S: UniversalRead>(
    fs: &impl UniversalReadFs<File = S>,
    directory: &Path,
) -> OperationResult<usize> {
    let status = TypedStorage::<S, DynamicFlagsStatus>::new(fs.open(
        status_file(directory),
        bitslice_open_options(Populate::No),
        Default::default(),
    )?);
    Ok(status
        .read_whole()?
        .first()
        .map_or(0, DynamicFlagsStatus::len))
}

impl InMemoryBitvecFlags {
    /// Schedule background prefetch of the two files [`Self::open`] reads.
    pub fn preopen(fs: &impl CachedReadFs, directory: &Path) -> OperationResult<()> {
        // Status file
        fs.schedule_prefetch(
            &status_file(directory),
            Some(bitslice_open_options(Populate::PreferBackground)),
            None,
        )?;

        // Bitslice
        fs.schedule_prefetch(
            &directory.join(FLAGS_FILE),
            Some(bitslice_open_options(Populate::PreferBackground)),
            None,
        )?;
        Ok(())
    }

    /// Open persisted flags read-only into an owned `BitVec`; creates and writes
    /// nothing. The flags file is padded past the logical length (held in the
    /// status file), so the bitvec is truncated to it and `count` is exact.
    pub fn open<S: UniversalRead>(
        fs: &impl UniversalReadFs<File = S>,
        directory: &Path,
    ) -> OperationResult<Self> {
        let len = read_logical_len::<S>(fs, directory)?;

        let flags_path = directory.join(FLAGS_FILE);
        let flags = StoredBitSlice::<S>::open(
            fs,
            &flags_path,
            bitslice_open_options(Populate::No),
            Default::default(),
        )?;
        let bits = flags.read_all()?;
        let bitvec = bits.get(..len).map(BitVec::from_bitslice).ok_or_else(|| {
            OperationError::service_error(format!(
                "Flags file {} holds fewer than {len} bits",
                flags_path.display(),
            ))
        })?;
        let count = bitvec.count_ones();

        Ok(Self {
            bitvec,
            count,
            directory: Some(directory.to_path_buf()),
        })
    }

    /// Wrap an already-materialized deletion `bitvec`, computing the set-flag
    /// count. For flags coming from an on-disk format other than the dynamic
    /// flags read by [`Self::open`] (e.g. the immutable dense `deleted.dat`).
    pub fn from_bitvec(bitvec: BitVec) -> Self {
        let count = bitvec.count_ones();
        Self {
            bitvec,
            count,
            directory: None,
        }
    }

    /// Whether the flag at `key` is set; out-of-range keys read as unset.
    pub fn get(&self, key: PointOffsetType) -> bool {
        self.bitvec.get(key as usize).is_some_and(|bit| *bit)
    }

    /// Number of set flags.
    pub fn count(&self) -> usize {
        self.count
    }

    /// Set flags as a `BitSlice`.
    pub fn as_bitslice(&self) -> &BitSlice {
        self.bitvec.as_bitslice()
    }

    /// Set `points`, growing as needed and keeping `count` in sync. Folds a
    /// live-reload deletion delta; live offsets aren't passed (they read unset).
    pub fn insert_all(&mut self, points: &[PointOffsetType]) {
        for &point in points {
            let index = point as usize;
            if index >= self.bitvec.len() {
                self.bitvec.resize(index + 1, false);
            }
            if !self.bitvec.replace(index, true) {
                self.count += 1;
            }
        }
    }

    /// Apply a live-reload delta: set `deleted_points`, then fold persisted
    /// deletion bits for `new_points` from the flags file.
    ///
    /// Whole-point deletions come from the id-tracker. An appended point can
    /// also carry a per-vector deletion that exists only on disk, for example a
    /// missing named vector stored as a placeholder with its slot marked
    /// deleted. That bit never appears in `deleted_points`. Already-set
    /// in-memory flags are never cleared, so unflushed deletions survive.
    pub fn live_reload<S: UniversalRead>(
        &mut self,
        fs: &impl UniversalReadFs<File = S>,
        deleted_points: &[PointOffsetType],
        new_points: &[PointOffsetType],
    ) -> OperationResult<()> {
        self.insert_all(deleted_points);
        self.fold_persisted_deletions(fs, new_points)
    }

    /// Fold on-disk deletion bits for `new_points` into the in-memory set.
    ///
    /// Only bits within the logical flag length from the status file are read;
    /// padding past that length is ignored.
    fn fold_persisted_deletions<S: UniversalRead>(
        &mut self,
        fs: &impl UniversalReadFs<File = S>,
        new_points: &[PointOffsetType],
    ) -> OperationResult<()> {
        let Some(directory) = &self.directory else {
            return Ok(());
        };
        if new_points.is_empty() {
            return Ok(());
        }

        let len = read_logical_len::<S>(fs, directory)?;
        let start = new_points[0] as usize;
        if start >= len {
            return Ok(());
        }
        let end = (new_points[new_points.len() - 1] as usize + 1).min(len);

        let flags = StoredBitSlice::<S>::open(
            fs,
            directory.join(FLAGS_FILE),
            bitslice_open_options(Populate::No),
            Default::default(),
        )?;
        let bits = flags.read_bit_range(start as u64..end as u64)?;

        let persisted: Vec<PointOffsetType> = new_points
            .iter()
            .copied()
            .take_while(|&point| (point as usize) < len)
            .filter(|&point| bits.get(point as usize - start).is_some_and(|bit| *bit))
            .collect();
        self.insert_all(&persisted);

        Ok(())
    }
}

#[allow(clippy::default_constructed_unit_structs)]
#[duplicate::duplicate_item(
    tests_mod       S               Fs              cfg_predicate;
    [tests_mmap]    [MmapFile]      [MmapFs]        [cfg(all())];
    [tests_uring]   [IoUringFile]   [IoUringFs]     [cfg(target_os = "linux")];
)]
#[cfg_predicate]
#[cfg(test)]
mod tests_mod {
    use std::iter;

    #[cfg_predicate]
    use common::universal_io::{Fs, S};
    use rand::prelude::StdRng;
    use rand::{RngExt, SeedableRng};
    use tempfile::Builder;

    use super::*;
    use crate::common::flags::dynamic_stored_flags::DynamicStoredFlags;

    /// Persist `flags` via the writable storage so the read-only path can open it.
    fn persist(fs: &Fs, dir: &Path, flags: &[bool]) {
        let mut dynamic_flags = DynamicStoredFlags::<S>::open(fs, dir, Populate::No).unwrap();
        dynamic_flags.set_len(fs, flags.len()).unwrap();
        flags
            .iter()
            .enumerate()
            .filter(|(_, flag)| **flag)
            .for_each(|(i, _)| assert!(!dynamic_flags.set(i, true).unwrap()));
        dynamic_flags.flusher()().unwrap();
    }

    #[test]
    fn open_materializes_persisted_flags() {
        let dir = Builder::new().prefix("storage_dir").tempdir().unwrap();
        let num_flags = 5003; // Prime number, not byte aligned
        let mut rng = StdRng::seed_from_u64(42);
        let random_flags: Vec<bool> = iter::repeat_with(|| rng.random()).take(num_flags).collect();

        persist(&Fs::default(), dir.path(), &random_flags);

        let flags = InMemoryBitvecFlags::open::<S>(&Fs::default(), dir.path()).unwrap();

        let expected_count = random_flags.iter().filter(|flag| **flag).count();
        assert_eq!(flags.count(), expected_count);
        for (i, &flag) in random_flags.iter().enumerate() {
            assert_eq!(flags.get(i as PointOffsetType), flag);
        }
    }

    #[test]
    fn insert_all_folds_deletion_delta() {
        let dir = Builder::new().prefix("storage_dir").tempdir().unwrap();
        let num_flags = 1000;
        let mut rng = StdRng::seed_from_u64(7);
        let random_flags: Vec<bool> = iter::repeat_with(|| rng.random()).take(num_flags).collect();

        persist(&Fs::default(), dir.path(), &random_flags);
        let mut flags = InMemoryBitvecFlags::open::<S>(&Fs::default(), dir.path()).unwrap();
        let base_count = flags.count();

        // One offset already set, one unset in range, one past the end (grows).
        let already_set = random_flags.iter().position(|flag| *flag).unwrap();
        let unset = random_flags.iter().position(|flag| !*flag).unwrap();
        let beyond = num_flags as PointOffsetType + 5;
        flags.insert_all(&[
            already_set as PointOffsetType,
            unset as PointOffsetType,
            beyond,
        ]);

        // Only the two newly-set offsets bump the count.
        assert_eq!(flags.count(), base_count + 2);
        assert!(flags.get(already_set as PointOffsetType));
        assert!(flags.get(unset as PointOffsetType));
        assert!(flags.get(beyond));
        assert!(!flags.get(beyond + 1));
    }

    #[test]
    fn live_reload_folds_persisted_deletion_on_appended_offsets() {
        let dir = Builder::new().prefix("storage_dir").tempdir().unwrap();
        let fs = Fs::default();
        persist(&fs, dir.path(), &[false, false, true]);

        let mut flags = InMemoryBitvecFlags::open::<S>(&fs, dir.path()).unwrap();
        assert_eq!(flags.count(), 1);

        // Writer appends two offsets and deletes only the second.
        {
            let mut dynamic_flags =
                DynamicStoredFlags::<S>::open(&fs, dir.path(), Populate::No).unwrap();
            dynamic_flags.set_len(&fs, 5).unwrap();
            assert!(!dynamic_flags.set(4, true).unwrap());
            dynamic_flags.flusher()().unwrap();
        }

        flags.live_reload::<S>(&fs, &[], &[3, 4]).unwrap();

        assert!(!flags.get(0));
        assert!(flags.get(2));
        assert!(!flags.get(3), "live appended offset stays unset");
        assert!(flags.get(4), "persisted deletion on appended offset");
        assert_eq!(flags.count(), 2);
    }

    #[test]
    fn live_reload_ignores_padding_past_logical_len() {
        let dir = Builder::new().prefix("storage_dir").tempdir().unwrap();
        let fs = Fs::default();
        persist(&fs, dir.path(), &[true, false, false]);

        // The flags file is preallocated past the logical length. Plant a set
        // bit in that padding; live-reload must not treat it as a deletion.
        let padding_bit = 500u64;
        {
            let mut stored = StoredBitSlice::<S>::open(
                &fs,
                dir.path().join(FLAGS_FILE),
                OpenOptions {
                    writeable: true,
                    need_sequential: false,
                    populate: Populate::No,
                    advice: AdviceSetting::Global,
                },
                Default::default(),
            )
            .unwrap();
            stored.replace_bit(padding_bit, true).unwrap();
            stored.flusher()().unwrap();
        }

        let mut flags = InMemoryBitvecFlags::open::<S>(&fs, dir.path()).unwrap();
        assert!(flags.get(0));
        assert!(!flags.get(padding_bit as PointOffsetType));

        flags
            .live_reload::<S>(&fs, &[], &[1, padding_bit as PointOffsetType])
            .unwrap();

        assert!(flags.get(0));
        assert!(
            !flags.get(padding_bit as PointOffsetType),
            "padding past logical len must not be folded in"
        );
        assert_eq!(flags.count(), 1);
    }

    #[test]
    fn live_reload_keeps_unflushed_in_memory_deletions() {
        let dir = Builder::new().prefix("storage_dir").tempdir().unwrap();
        let fs = Fs::default();
        persist(&fs, dir.path(), &[false, false, false, false]);

        let mut flags = InMemoryBitvecFlags::open::<S>(&fs, dir.path()).unwrap();
        // Id-tracker deletion applied in memory, not yet on disk.
        flags.insert_all(&[1]);
        assert!(flags.get(1));

        // Disk still has no deletions; appended offset 3 is live on disk.
        flags.live_reload::<S>(&fs, &[], &[2, 3]).unwrap();

        assert!(
            flags.get(1),
            "unflushed in-memory deletion must not be lost"
        );
        assert!(!flags.get(2));
        assert!(!flags.get(3));
        assert_eq!(flags.count(), 1);
    }
}
