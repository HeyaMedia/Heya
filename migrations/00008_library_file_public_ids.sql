-- +goose Up
ALTER TABLE public.library_files ADD COLUMN IF NOT EXISTS public_id uuid;

UPDATE public.library_files
   SET public_id = gen_random_uuid()
 WHERE public_id IS NULL;

ALTER TABLE public.library_files
  ALTER COLUMN public_id SET DEFAULT gen_random_uuid(),
  ALTER COLUMN public_id SET NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_library_files_public_id
  ON public.library_files USING btree (public_id);

-- +goose Down
DROP INDEX IF EXISTS public.idx_library_files_public_id;

ALTER TABLE public.library_files DROP COLUMN IF EXISTS public_id;
