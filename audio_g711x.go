package main

/*************************************************/
/* audio
/*************************************************/
//AudioData see video_file_format_spec_v10.pdf
type AudioDataG711 struct {
	SoundFormat uint8 //4bit
	SoundRate   uint8 //2bit
	SoundSize   uint8 //1bit
	SoundType   uint8 //1bit
	SoundData   []byte
}
